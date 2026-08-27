package store

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
)

// dropMetaInputLimitColumn rewrites embedding_meta in the pre-v8 shape, without
// max_input_bytes, and sets the schema version to storedVersion. It builds the
// fixture the v7 -> v8 migration has to upgrade.
func dropMetaInputLimitColumn(t *testing.T, dbPath string, storedVersion int) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	}()

	stmts := []string{
		"ALTER TABLE embedding_meta DROP COLUMN max_input_bytes",
		"UPDATE schema_version SET version = " + strconv.Itoa(storedVersion),
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("build pre-v8 fixture (%s): %v", stmt, err)
		}
	}
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestMetaInputLimitMigrationPreservesIndexedData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v7.db")

	s := openTestStoreAt(t, dbPath)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
	wantRows := seedVecFixture(t, s)
	if err := s.UpdateMeta("src-a", "model-v1", 3, QuantizationInt8, testMaxInputBytes); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	dropMetaInputLimitColumn(t, dbPath, 7)

	s2 := openTestStoreAt(t, dbPath)
	if v := storedSchemaVersion(t, s2); v != schemaVersion {
		t.Fatalf("schema_version=%d after migration, want %d", v, schemaVersion)
	}
	if got := countRows(t, s2, "embeddings"); got != wantRows {
		t.Fatalf("embeddings=%d after migration, want %d", got, wantRows)
	}
	if got := countRows(t, s2, "vec_embeddings"); got != wantRows {
		t.Fatalf("vec_embeddings=%d after migration, want %d", got, wantRows)
	}
	if got := countRows(t, s2, "chunks_fts_docsize"); got != wantRows {
		t.Fatalf("indexed FTS rows=%d after migration, want %d", got, wantRows)
	}

	// The old row keeps the column default, which no valid configuration
	// produces, so the source rebuilds under the configured limit.
	mc, err := s2.ModelChanged("src-a", "model-v1", 3, QuantizationInt8, testMaxInputBytes)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if !mc.Changed {
		t.Fatal("migrated source reports no change; it would keep chunks cut to an unknown limit")
	}
	if mc.OldMaxInputBytes != 0 {
		t.Fatalf("OldMaxInputBytes=%d after migration, want 0", mc.OldMaxInputBytes)
	}
}

func TestMetaInputLimitMigrationIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v8-retry.db")

	s := openTestStoreAt(t, dbPath)
	if err := s.UpdateMeta("src", "model-v1", 3, QuantizationInt8, testMaxInputBytes); err != nil {
		t.Fatalf("update meta: %v", err)
	}

	// An earlier deferred step can hold the stored version at 7 after the
	// column already landed, so the step is planned again on a v8 table.
	for range 2 {
		if !s.migrateMetaInputLimit() {
			t.Fatal("migration on a table that already has the column")
		}
	}

	mc, err := s.ModelChanged("src", "model-v1", 3, QuantizationInt8, testMaxInputBytes)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if mc.Changed || mc.OldMaxInputBytes != testMaxInputBytes {
		t.Fatalf("stored limit lost by a repeated migration: %+v", mc)
	}
}

func TestMetaInputLimitMigrationRetriesAfterInterruption(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v7-retry.db")

	s := openTestStoreAt(t, dbPath)
	if err := s.UpdateMeta("src", "model-v1", 3, QuantizationInt8, testMaxInputBytes); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// A process killed between the column change and the version write leaves
	// the column in place at version 7. The next start must reach 8 rather than
	// fail on a duplicate column.
	dropMetaInputLimitColumn(t, dbPath, 7)
	s2 := openTestStoreAt(t, dbPath)
	if v := storedSchemaVersion(t, s2); v != schemaVersion {
		t.Fatalf("schema_version=%d after first migration, want %d", v, schemaVersion)
	}
	if _, err := s2.DB().Exec("UPDATE schema_version SET version = 7"); err != nil {
		t.Fatalf("rewind schema_version: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	s3 := openTestStoreAt(t, dbPath)
	if v := storedSchemaVersion(t, s3); v != schemaVersion {
		t.Fatalf("schema_version=%d after retried migration, want %d", v, schemaVersion)
	}
}

func TestModelChangedOnInputLimitChange(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpdateMeta("src", "model-v1", 3, QuantizationInt8, 8000); err != nil {
		t.Fatalf("update meta: %v", err)
	}

	mc, err := s.ModelChanged("src", "model-v1", 3, QuantizationInt8, 4000)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if !mc.Changed {
		t.Fatal("a changed max_input_bytes must rebuild the source")
	}
	if mc.OldMaxInputBytes != 8000 {
		t.Fatalf("OldMaxInputBytes=%d, want 8000", mc.OldMaxInputBytes)
	}

	mc, err = s.ModelChanged("src", "model-v1", 3, QuantizationInt8, 8000)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if mc.Changed {
		t.Fatal("an unchanged max_input_bytes must not rebuild the source")
	}
}
