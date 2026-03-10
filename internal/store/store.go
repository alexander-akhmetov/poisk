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
}

func Open(dbPath string, dimensions int) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db, dimensions: dimensions}

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
	var storedVersion int
	err := s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&storedVersion)
	if err != nil {
		slog.Info("schema version not found, initializing with full index",
			"want", schemaVersion)
		migrated = true
	} else if storedVersion != schemaVersion {
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

	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

	// Only write version after a migration to avoid a DELETE+INSERT window
	// where concurrent readers see an empty table and falsely trigger migration.
	if migrated {
		if _, err := s.db.Exec("DELETE FROM schema_version"); err != nil {
			return fmt.Errorf("delete schema_version: %w", err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion); err != nil {
			return fmt.Errorf("insert schema_version: %w", err)
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
	if _, err := s.db.Exec(fts5DDL); err != nil {
		slog.Warn("FTS5 not available", "error", err)
	} else {
		s.ftsAvailable = true
		s.backfillFTS5()
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
	rows, err := s.db.Query("SELECT DISTINCT dimensions FROM embedding_meta")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var storedDims int
			if rows.Scan(&storedDims) == nil && storedDims != s.dimensions {
				needsDrop = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			slog.Warn("rows iteration error", "error", err)
		}
	}
	if needsDrop {
		slog.Info("vec0 dimensions mismatch, recreating", "new", s.dimensions)
		if _, err := s.db.Exec("DROP TABLE IF EXISTS vec_embeddings"); err != nil {
			slog.Warn("failed to drop vec_embeddings", "error", err)
		}
		// Clear stale meta so mismatch isn't re-detected on next startup
		if _, err := s.db.Exec("DELETE FROM embedding_meta WHERE dimensions != ?", s.dimensions); err != nil {
			slog.Warn("failed to clean stale embedding_meta", "error", err)
		}
	}

	if _, err := s.db.Exec(vec0DDL(s.dimensions)); err != nil {
		slog.Warn("vec0 not available", "error", err)
		return
	}
	s.vecAvailable = true
}

func (s *Store) backfillFTS5() {
	// Only backfill if FTS5 table is empty but embeddings exist
	var ftsCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM chunks_fts").Scan(&ftsCount); err != nil {
		slog.Warn("FTS5 count check failed", "error", err)
		return
	}
	if ftsCount > 0 {
		return
	}

	_, err := s.db.Exec(`INSERT INTO chunks_fts(chunk_text, id, source, file_path, line_num, folder, end_line, language, chunk_kind, symbol)
		SELECT chunk_text, CAST(id AS TEXT), source, file_path, CAST(line_num AS TEXT), COALESCE(folder, ''),
		       CAST(end_line AS TEXT), language, chunk_kind, symbol
		FROM embeddings`)
	if err != nil {
		slog.Warn("FTS5 backfill failed", "error", err)
	}
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
