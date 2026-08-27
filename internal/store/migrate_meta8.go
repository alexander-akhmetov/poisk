package store

import (
	"fmt"
	"log/slog"
)

// migrateMetaInputLimit upgrades a v7 embedding_meta table to the v8 layout,
// which records the per-input byte limit each source was chunked under.
//
// The column is added in place, so embeddings, FTS rows and vectors all stay.
// Existing rows get 0, which no valid configuration produces, so the indexer's
// existing model-change path rebuilds each source under the configured limit.
//
// It reports whether the migration completed; a failure leaves the stored
// schema version at 7 for a later retry.
func (s *Store) migrateMetaInputLimit() bool {
	if err := s.addMetaInputLimitColumn(); err != nil {
		slog.Warn("embedding_meta input limit migration failed, deferring", "error", err)
		return false
	}
	return true
}

func (s *Store) addMetaInputLimitColumn() error {
	present, err := s.metaHasInputLimitColumn()
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	if _, err := s.db.Exec("ALTER TABLE embedding_meta ADD COLUMN max_input_bytes INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add embedding_meta.max_input_bytes: %w", err)
	}
	return nil
}

// metaHasInputLimitColumn reports whether the column is already there. An
// earlier deferred step can hold the stored version below 8 after the column
// landed, and re-running the ALTER would then fail as a duplicate column.
func (s *Store) metaHasInputLimitColumn() (bool, error) {
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('embedding_meta') WHERE name = 'max_input_bytes'",
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("read embedding_meta columns: %w", err)
	}
	return count > 0, nil
}
