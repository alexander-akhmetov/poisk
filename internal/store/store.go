package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
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
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

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

func (s *Store) initVec0() {
	// Check if ANY stored source has different dimensions than config.
	// If so, drop vec0 and let indexer rebuild incrementally.
	rows, err := s.db.Query("SELECT DISTINCT dimensions FROM embedding_meta")
	if err == nil {
		defer rows.Close()
		needsDrop := false
		for rows.Next() {
			var storedDims int
			if rows.Scan(&storedDims) == nil && storedDims != s.dimensions {
				needsDrop = true
				break
			}
		}
		if needsDrop {
			slog.Info("vec0 dimensions mismatch, recreating", "new", s.dimensions)
			s.db.Exec("DROP TABLE IF EXISTS vec_embeddings")
		}
	}

	if _, err := s.db.Exec(vec0DDL(s.dimensions)); err != nil {
		slog.Warn("vec0 not available", "error", err)
		return
	}
	s.vecAvailable = true
}

func (s *Store) backfillFTS5() {
	_, err := s.db.Exec(`INSERT INTO chunks_fts(chunk_text, id, source, file_path, line_num, folder)
		SELECT chunk_text, CAST(id AS TEXT), source, file_path, CAST(line_num AS TEXT), COALESCE(folder, '')
		FROM embeddings
		WHERE id NOT IN (SELECT CAST(id AS INTEGER) FROM chunks_fts)`)
	if err != nil {
		slog.Warn("FTS5 backfill failed", "error", err)
	}
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) VecAvailable() bool { return s.vecAvailable }
func (s *Store) FTSAvailable() bool { return s.ftsAvailable }
func (s *Store) DB() *sql.DB        { return s.db }

// Dimensions returns the configured embedding dimensions.
func (s *Store) Dimensions() int { return s.dimensions }

func splitStatements(ddl string) []string {
	var stmts []string
	for _, s := range strings.Split(ddl, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

