package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// migrateVecPartition upgrades a v6 vec_embeddings table to the v7 layout,
// which adds the source partition key.
//
// Vectors live only in vec_embeddings since the legacy blob column was dropped,
// but vec0 hands them back as raw typed bytes, so v7 re-partitions the existing
// vectors instead of re-embedding: it stages rowid, source and embedding in a
// plain table, recreates vec_embeddings with the partition key, and copies the
// bytes back through vec_int8()/vec_f32(). About 200k int8[1024] vectors take
// ~45s, against ~70 minutes to re-embed them, and it needs no embedding
// endpoint.
//
// Everything runs in one transaction, so a failure leaves the old table in
// place and the caller keeps the stored schema version at 6 for a later retry.
// It reports whether the migration completed.
func (s *Store) migrateVecPartition() bool {
	if !s.vecAvailable {
		slog.Warn("vec0 not available, deferring vec partition migration")
		return false
	}

	if err := s.runVecPartitionMigration(); err != nil {
		slog.Warn("vec partition migration failed, deferring", "error", err)
		return false
	}
	return true
}

// vecTableIsPartitioned reports whether vec_embeddings already carries the
// source partition key. The migration is idempotent but expensive (~30s on a
// 200k-vector index), and an earlier deferred step holds the stored version
// below 7, so without this check every process start would copy every vector
// again.
func (s *Store) vecTableIsPartitioned() (bool, error) {
	var ddl string
	err := s.db.QueryRow("SELECT sql FROM sqlite_master WHERE name = 'vec_embeddings'").Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read vec_embeddings ddl: %w", err)
	}
	return strings.Contains(ddl, "partition key"), nil
}

func (s *Store) runVecPartitionMigration() error {
	partitioned, err := s.vecTableIsPartitioned()
	if err != nil {
		return err
	}
	if partitioned {
		return nil
	}

	ctx := context.Background()

	// One dedicated connection under BEGIN IMMEDIATE. The staging table lives in
	// the temp database, so a deferred transaction would first take a read lock
	// and only ask for the write lock at DROP TABLE vec_embeddings; SQLite fails
	// that upgrade with SQLITE_BUSY at once instead of waiting out busy_timeout,
	// which would leave the store without vector writes for the whole run.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
			slog.Warn("rolling back vec partition migration failed", "error", err)
		}
	}()

	// A temp staging table keeps a copy of every vector out of the main database
	// file. A plain table writes them there through the WAL and the pages stay on
	// the freelist until a VACUUM, which cost +28.7 MB on a 21 MB fixture.
	steps := []struct {
		what string
		stmt string
	}{
		{"create staging table", "CREATE TEMP TABLE vec_migrate_tmp (rowid INTEGER PRIMARY KEY, source TEXT NOT NULL, embedding BLOB NOT NULL)"},
		// Vectors whose embeddings row is gone are orphans; the JOIN drops them.
		{"stage vectors", `INSERT INTO vec_migrate_tmp (rowid, source, embedding)
			SELECT v.rowid, e.source, v.embedding
			FROM vec_embeddings v JOIN embeddings e ON e.id = v.rowid`},
		{"drop vec_embeddings", "DROP TABLE vec_embeddings"},
		{"recreate vec_embeddings", vec0DDL(s.dimensions, s.quantization)},
		{"copy vectors back", "INSERT INTO vec_embeddings (rowid, source, embedding) SELECT rowid, source, " +
			vecValueCtor(s.quantization, "embedding") + " FROM vec_migrate_tmp"},
		{"drop staging table", "DROP TABLE vec_migrate_tmp"},
	}
	for _, step := range steps {
		if _, err := conn.ExecContext(ctx, step.stmt); err != nil {
			return fmt.Errorf("%s: %w", step.what, err)
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}
