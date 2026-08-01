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

// migrationPlan says which upgrade steps a stored schema version needs.
// A targeted step preserves the indexed data; rebuild drops everything and
// forces a full reindex.
type migrationPlan struct {
	rebuild bool // no targeted path exists: drop all tables
	fts     bool // v5 -> v6: rebuild chunks_fts in the external-content layout
	vecPart bool // v6 -> v7: re-partition vec_embeddings by source
}

func (p migrationPlan) any() bool { return p.rebuild || p.fts || p.vecPart }

// planMigration walks every version between the stored one and the current one
// and collects the step for each. A version with no targeted step forces a full
// rebuild, which supersedes the targeted steps. Walking the range keeps every
// step live across future version bumps; a literal version pair would go dead
// on the next one.
func planMigration(storedVersion int) migrationPlan {
	var plan migrationPlan
	if storedVersion > schemaVersion {
		return migrationPlan{rebuild: true}
	}
	for v := storedVersion; v < schemaVersion; v++ {
		switch v {
		case 5:
			plan.fts = true
		case 6:
			plan.vecPart = true
		default:
			return migrationPlan{rebuild: true}
		}
	}
	return plan
}

func (s *Store) initSchema() error {
	// Schema version table always exists
	if _, err := s.db.Exec(schemaVersionDDL); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	// Check current schema version to decide whether migration is needed.
	var storedVersion int
	var plan migrationPlan
	if err := s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&storedVersion); err != nil {
		slog.Info("schema version not found, initializing with full index", "want", schemaVersion)
		plan = migrationPlan{rebuild: true}
	} else {
		plan = planMigration(storedVersion)
		if plan.rebuild {
			slog.Info(fmt.Sprintf("schema version mismatch (stored=%d, want=%d), dropping all tables for full reindex",
				storedVersion, schemaVersion))
		} else if plan.any() {
			slog.Info("migrating schema", "stored", storedVersion, "want", schemaVersion,
				"rebuild_fts", plan.fts, "repartition_vectors", plan.vecPart)
		}
	}

	if plan.rebuild {
		s.dropAllTables()
		// Recreate schema version table after drop
		if _, err := s.db.Exec(schemaVersionDDL); err != nil {
			return fmt.Errorf("recreate schema_version: %w", err)
		}
	}
	if plan.fts {
		// The new external-content table is created below by fts5DDL and
		// repopulated by backfillFTS5. Dropping a virtual table requires its
		// module, so a build without FTS5 cannot drop the old table; defer the
		// migration (version stays 5) so an FTS5-enabled build retries it.
		if _, err := s.db.Exec("DROP TABLE IF EXISTS chunks_fts"); err != nil {
			slog.Warn("cannot drop chunks_fts, deferring FTS migration", "error", err)
			plan.fts = false
		}
	}

	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

	// Only write version after a migration to avoid a DELETE+INSERT window
	// where concurrent readers see an empty table and falsely trigger migration.
	if plan.rebuild {
		if err := s.writeSchemaVersion(schemaVersion); err != nil {
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

	// vec0 re-partition, on the table initVec0 just confirmed
	vecReady := false
	if plan.vecPart {
		vecReady = s.migrateVecPartition()
		if !vecReady {
			// vec_embeddings still has the v6 layout, with no source column,
			// so every insert into it would fail. Turn vector support off for
			// this process rather than report a healthy store that cannot
			// write vectors; the next start retries the migration.
			slog.Warn("vector search off until the vec partition migration completes; " +
				"anything indexed meanwhile gets no vectors and needs a reindex afterwards")
			s.vecAvailable = false
		}
	}

	// FTS5
	ftsReady := false
	if _, err := s.db.Exec(fts5DDL); err != nil {
		slog.Warn("FTS5 not available", "error", err)
	} else {
		s.ftsAvailable = true
		ftsReady = s.backfillFTS5()
	}

	if plan.rebuild {
		return nil
	}
	reached := plan.reachedVersion(storedVersion, ftsReady, vecReady)
	if reached == storedVersion {
		return nil
	}
	return s.writeSchemaVersion(reached)
}

// reachedVersion reports the version the database actually reached. A targeted
// migration counts only once its data is in place, so a failed step leaves the
// stored version behind and the next start retries it instead of leaving an
// empty index behind a healthy-looking database. The version advances along the
// contiguous run of successful steps only: a v5 database whose FTS rebuild
// failed stays at 5 even if the later vec step succeeded.
func (p migrationPlan) reachedVersion(storedVersion int, ftsReady, vecReady bool) int {
	// One entry per targeted step, in version order. Each advances a database
	// at from to from+1.
	steps := []struct {
		from      int
		planned   bool
		succeeded bool
	}{
		{from: 5, planned: p.fts, succeeded: ftsReady},
		{from: 6, planned: p.vecPart, succeeded: vecReady},
	}

	reached := storedVersion
	for _, step := range steps {
		if step.from < reached {
			continue // already past this step
		}
		if step.from != reached || !step.planned || !step.succeeded {
			break
		}
		reached = step.from + 1
	}
	return reached
}

func (s *Store) writeSchemaVersion(version int) error {
	if _, err := s.db.Exec("DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("delete schema_version: %w", err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", version); err != nil {
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
