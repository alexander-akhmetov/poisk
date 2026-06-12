package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

func init() {
	sqlite_vec.Auto()
}

type Store struct {
	db           *sql.DB
	vecAvailable bool
	ftsAvailable bool
	dimensions   int
	quantization string
}

func Open(dbPath string, dimensions int, quantization string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db, dimensions: dimensions, quantization: quantization}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) initSchema() error {
	// Schema version table always exists
	if _, err := s.db.Exec(schemaVersionDDL); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	// Check current schema version to decide whether migration is needed.
	migrated := false
	ftsOnlyMigration := false
	var storedVersion int
	err := s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&storedVersion)
	switch {
	case err != nil:
		slog.Info("schema version not found, initializing with full index",
			"want", schemaVersion)
		migrated = true
	case storedVersion == 5 && schemaVersion == 6:
		// v6 only changed the chunks_fts layout (external content); embeddings
		// and vectors are unaffected, so rebuild the FTS table instead of
		// dropping everything and re-embedding.
		slog.Info("schema version 5 -> 6, rebuilding FTS index only")
		ftsOnlyMigration = true
	case storedVersion != schemaVersion:
		slog.Info(fmt.Sprintf("schema version mismatch (stored=%d, want=%d), dropping all tables for full reindex",
			storedVersion, schemaVersion))
		migrated = true
	}

	if migrated {
		s.dropAllTables()
		// Recreate schema version table after drop
		if _, err := s.db.Exec(schemaVersionDDL); err != nil {
			return fmt.Errorf("recreate schema_version: %w", err)
		}
	}
	if ftsOnlyMigration {
		// The new external-content table is created below by fts5DDL and
		// repopulated by backfillFTS5. Dropping a virtual table requires its
		// module, so a build without FTS5 cannot drop the old table; defer the
		// migration (version stays 5) so an FTS5-enabled build retries it.
		if _, err := s.db.Exec("DROP TABLE IF EXISTS chunks_fts"); err != nil {
			slog.Warn("cannot drop chunks_fts, deferring FTS migration", "error", err)
			ftsOnlyMigration = false
		}
	}

	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

	// Only write version after a migration to avoid a DELETE+INSERT window
	// where concurrent readers see an empty table and falsely trigger migration.
	if migrated {
		if err := s.writeSchemaVersion(); err != nil {
			return err
		}
	}

	// Indexing progress (not versioned — transient runtime state)
	if _, err := s.db.Exec(indexingProgressDDL); err != nil {
		return fmt.Errorf("create indexing_progress: %w", err)
	}

	// Drop legacy embedding blob column if it exists.
	s.dropEmbeddingColumn()

	// vec0 — drop and recreate if dimensions changed
	s.initVec0()

	// FTS5
	ftsReady := false
	if _, err := s.db.Exec(fts5DDL); err != nil {
		slog.Warn("FTS5 not available", "error", err)
	} else {
		s.ftsAvailable = true
		ftsReady = s.backfillFTS5()
	}

	// The FTS-only migration writes the version only after the index is
	// rebuilt; if the rebuild failed the version stays 5 and the next start
	// retries the migration instead of leaving an empty index behind a
	// healthy-looking v6 database.
	if ftsOnlyMigration && ftsReady {
		if err := s.writeSchemaVersion(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) writeSchemaVersion() error {
	if _, err := s.db.Exec("DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("delete schema_version: %w", err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion); err != nil {
		return fmt.Errorf("insert schema_version: %w", err)
	}
	return nil
}

func (s *Store) dropAllTables() {
	tables := []string{
		"vec_embeddings", "chunks_fts", "embeddings",
		"embedding_files", "embedding_meta", "schema_version",
	}
	for _, t := range tables {
		if _, err := s.db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
			slog.Warn("failed to drop table", "table", t, "error", err)
		}
	}
}

func (s *Store) initVec0() {
	needsDrop := false
	rows, err := s.db.Query("SELECT DISTINCT dimensions, quantization FROM embedding_meta")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var storedDims int
			var storedQuant string
			if rows.Scan(&storedDims, &storedQuant) == nil && (storedDims != s.dimensions || storedQuant != s.quantization) {
				needsDrop = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			slog.Warn("rows iteration error", "error", err)
		}
	}
	if needsDrop {
		slog.Info("vec0 dimensions or quantization mismatch, recreating",
			"new_dims", s.dimensions, "new_quantization", s.quantization)
		if _, err := s.db.Exec("DROP TABLE IF EXISTS vec_embeddings"); err != nil {
			slog.Warn("failed to drop vec_embeddings", "error", err)
		}
		// Clear stale meta so mismatch isn't re-detected on next startup
		if _, err := s.db.Exec("DELETE FROM embedding_meta WHERE dimensions != ? OR quantization != ?", s.dimensions, s.quantization); err != nil {
			slog.Warn("failed to clean stale embedding_meta", "error", err)
		}
	}

	if _, err := s.db.Exec(vec0DDL(s.dimensions, s.quantization)); err != nil {
		slog.Warn("vec0 not available", "error", err)
		return
	}
	s.vecAvailable = true
}

// backfillFTS5 rebuilds the FTS index from embeddings if it is empty and
// reports whether the index is populated afterwards.
func (s *Store) backfillFTS5() bool {
	// Only rebuild if the FTS index is empty. A plain COUNT(*) on an
	// external-content table reads rows from embeddings, so probe the docsize
	// shadow table, which tracks the index itself.
	var ftsCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM chunks_fts_docsize").Scan(&ftsCount); err != nil {
		slog.Warn("FTS5 count check failed", "error", err)
		return false
	}
	if ftsCount > 0 {
		return true
	}

	if _, err := s.db.Exec("INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')"); err != nil {
		slog.Warn("FTS5 rebuild failed", "error", err)
		return false
	}
	return true
}

func (s *Store) dropEmbeddingColumn() {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('embeddings') WHERE name = 'embedding'").Scan(&count)
	if err != nil || count == 0 {
		return
	}
	slog.Info("dropping legacy embedding blob column")
	if _, err := s.db.Exec("ALTER TABLE embeddings DROP COLUMN embedding"); err != nil {
		slog.Warn("failed to drop embedding column", "error", err)
	}
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) VecAvailable() bool { return s.vecAvailable }
func (s *Store) FTSAvailable() bool { return s.ftsAvailable }
func (s *Store) DB() *sql.DB        { return s.db }

// VecValueExpr returns the SQL expression that converts a bound float32 blob
// into the vec0 column's element type, for both INSERT values and MATCH
// operands. vec0 dispatches on value subtype: a plain blob parameter is always
// parsed as float32, so int8 columns need the blob wrapped in
// vec_quantize_int8, which quantizes and tags the result with the int8 subtype.
func (s *Store) VecValueExpr() string {
	if s.quantization == QuantizationInt8 {
		return "vec_quantize_int8(?, 'unit')"
	}
	return "?"
}

func splitStatements(ddl string) []string {
	var stmts []string
	for s := range strings.SplitSeq(ddl, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}
