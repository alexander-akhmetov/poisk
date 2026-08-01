package store

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// unpartitionVecTable rewrites vec_embeddings in the pre-v7 shape (no source
// partition key), preserving the stored vectors, and sets the schema version to
// storedVersion. It builds the fixture the v6 -> v7 migration has to upgrade.
func unpartitionVecTable(t *testing.T, dbPath string, dims, storedVersion int) {
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
		"CREATE TABLE v6_stage AS SELECT rowid AS rid, embedding FROM vec_embeddings",
		"DROP TABLE vec_embeddings",
		"CREATE VIRTUAL TABLE vec_embeddings USING vec0(embedding int8[" + strconv.Itoa(dims) + "] distance_metric=cosine)",
		"INSERT INTO vec_embeddings (rowid, embedding) SELECT rid, vec_int8(embedding) FROM v6_stage",
		"DROP TABLE v6_stage",
		"UPDATE schema_version SET version = " + strconv.Itoa(storedVersion),
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("build pre-v7 fixture (%s): %v", stmt, err)
		}
	}
}

type vecHit struct {
	rowid    int64
	distance float64
}

// unscopedKNN runs a KNN with no source constraint and returns the hits in
// order, for comparing behaviour across the partition-key change.
func unscopedKNN(t *testing.T, db *sql.DB, query []byte, k int) []vecHit {
	t.Helper()
	rows, err := db.Query(
		"SELECT rowid, distance FROM vec_embeddings WHERE embedding MATCH vec_quantize_int8(?, 'unit') AND k = ? ORDER BY distance",
		query, k,
	)
	if err != nil {
		t.Fatalf("unscoped knn: %v", err)
	}
	defer rows.Close()

	var hits []vecHit
	for rows.Next() {
		var h vecHit
		if err := rows.Scan(&h.rowid, &h.distance); err != nil {
			t.Fatalf("scan hit: %v", err)
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("knn rows: %v", err)
	}
	return hits
}

// unscopedKNNAt opens the database directly, without running a migration, so a
// pre-v7 fixture can be queried in its stored shape.
func unscopedKNNAt(t *testing.T, dbPath string, query []byte, k int) []vecHit {
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
	return unscopedKNN(t, db, query, k)
}

func vecTableDDL(t *testing.T, s *Store) string {
	t.Helper()
	var ddl string
	if err := s.DB().QueryRow("SELECT sql FROM sqlite_master WHERE name='vec_embeddings'").Scan(&ddl); err != nil {
		t.Fatalf("read vec_embeddings ddl: %v", err)
	}
	return ddl
}

func storedSchemaVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	return v
}

// seedVecFixture indexes three known vectors across two sources and returns the
// number of chunks written.
func seedVecFixture(t *testing.T, s *Store) int {
	t.Helper()
	if err := s.InsertEntries("src-a", "main.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "exact", Embedding: []float32{1, 0, 0}, Folder: "src-a", Language: "go"},
		{LineNum: 2, EndLine: 2, Text: "near", Embedding: []float32{0.8, 0.6, 0}, Folder: "src-a", Language: "go"},
	}); err != nil {
		t.Fatalf("insert src-a: %v", err)
	}
	if err := s.InsertEntries("src-b", "other.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "orthogonal", Embedding: []float32{0, 1, 0}, Folder: "src-b", Language: "go"},
	}); err != nil {
		t.Fatalf("insert src-b: %v", err)
	}
	return 3
}

func TestVecPartitionMigrationPreservesVectorsWithoutReEmbedding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6.db")

	s := openTestStoreAt(t, dbPath)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
	wantRows := seedVecFixture(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	unpartitionVecTable(t, dbPath, 3, 6)

	// The unscoped query must behave identically before and after the partition
	// key, so capture the v6 answer first.
	queryBlob := Float32sToBlob([]float32{1, 0, 0})
	before := unscopedKNNAt(t, dbPath, queryBlob, wantRows)

	// Reopening runs the migration. No embedding endpoint is configured or
	// needed: the vectors are copied, not recomputed.
	s2 := openTestStoreAt(t, dbPath)
	if !s2.VecAvailable() {
		t.Fatal("vec0 unavailable after migration")
	}
	if v := storedSchemaVersion(t, s2); v != schemaVersion {
		t.Fatalf("schema_version=%d after migration, want %d", v, schemaVersion)
	}
	if ddl := vecTableDDL(t, s2); !strings.Contains(ddl, "source TEXT partition key") {
		t.Fatalf("vec_embeddings not partitioned after migration: %s", ddl)
	}

	var vecCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount != wantRows {
		t.Fatalf("vec_embeddings count=%d, want %d", vecCount, wantRows)
	}
	chunkCount := 0
	for _, src := range []string{"src-a", "src-b"} {
		n, err := s2.Count(src)
		if err != nil {
			t.Fatalf("count %s: %v", src, err)
		}
		chunkCount += n
	}
	if chunkCount != wantRows {
		t.Fatalf("embeddings count=%d, want %d", chunkCount, wantRows)
	}

	// Sources moved into the partition column, matching the joined rows.
	var mismatched int
	if err := s2.DB().QueryRow(
		"SELECT COUNT(*) FROM vec_embeddings v JOIN embeddings e ON e.id = v.rowid WHERE v.source != e.source",
	).Scan(&mismatched); err != nil {
		t.Fatalf("compare sources: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("%d migrated vectors carry the wrong source", mismatched)
	}

	// Same ordered rowids and distances as the unpartitioned table.
	after := unscopedKNN(t, s2.DB(), queryBlob, wantRows)
	if len(after) != len(before) {
		t.Fatalf("unscoped knn returned %d hits after migration, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].rowid != before[i].rowid || after[i].distance != before[i].distance {
			t.Fatalf("unscoped hit %d changed: %+v -> %+v", i, before[i], after[i])
		}
	}

	// A self-match must come back at distance ~0: the bytes round-tripped.
	var text string
	var distance float64
	if err := s2.DB().QueryRow(
		"SELECT e.chunk_text, v.distance FROM vec_embeddings v JOIN embeddings e ON e.id = v.rowid"+
			" WHERE v.embedding MATCH "+s2.VecValueExpr()+" AND k = 3 ORDER BY v.distance LIMIT 1",
		queryBlob,
	).Scan(&text, &distance); err != nil {
		t.Fatalf("self-match query: %v", err)
	}
	if text != "exact" {
		t.Fatalf("nearest chunk=%q, want %q", text, "exact")
	}
	if distance > 0.000001 {
		t.Fatalf("self-match distance=%v, want <= 0.000001", distance)
	}

	// Scoping to one partition returns only that source's vectors.
	rows, err := s2.DB().Query(
		"SELECT e.chunk_text FROM vec_embeddings v JOIN embeddings e ON e.id = v.rowid"+
			" WHERE v.embedding MATCH "+s2.VecValueExpr()+" AND k = 3 AND v.source = ? ORDER BY v.distance",
		queryBlob, "src-b",
	)
	if err != nil {
		t.Fatalf("scoped knn query: %v", err)
	}
	defer rows.Close()
	var scoped []string
	for rows.Next() {
		var chunkText string
		if err := rows.Scan(&chunkText); err != nil {
			t.Fatalf("scan scoped row: %v", err)
		}
		scoped = append(scoped, chunkText)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scoped rows: %v", err)
	}
	if len(scoped) != 1 || scoped[0] != "orthogonal" {
		t.Fatalf("scoped knn returned %v, want [orthogonal]", scoped)
	}
}

func TestVecPartitionMigrationDeferredWhenCopyBackFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6-fail.db")

	s := openTestStoreAt(t, dbPath)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
	wantRows := seedVecFixture(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	unpartitionVecTable(t, dbPath, 3, 6)

	// Opening with a different vector width recreates vec_embeddings at 4
	// dimensions, so copying the staged 3-dimension vectors back fails. No
	// embedding_meta rows exist, so initVec0 does not notice the mismatch first.
	failed, err := Open(dbPath, 4, QuantizationInt8)
	if err != nil {
		t.Fatalf("open with mismatched dimensions: %v", err)
	}
	if v := storedSchemaVersion(t, failed); v != 6 {
		t.Fatalf("schema_version=%d after failed migration, want 6", v)
	}
	// vec_embeddings still has no source column, so the store must report vector
	// search as unavailable instead of failing every write for the whole run.
	if failed.VecAvailable() {
		t.Fatal("VecAvailable() is true while vec_embeddings still has the pre-v7 layout")
	}
	if err := failed.InsertEntries("src-a", "added.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "written while deferred", Embedding: []float32{1, 0, 0, 0}, Folder: "src-a", Language: "go"},
	}); err != nil {
		t.Fatalf("insert while the vec migration is deferred: %v", err)
	}
	var vecCount int
	if err := failed.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount != wantRows {
		t.Fatalf("vec_embeddings count=%d after rollback, want %d", vecCount, wantRows)
	}
	if ddl := vecTableDDL(t, failed); strings.Contains(ddl, "partition key") {
		t.Fatalf("vec_embeddings must stay unpartitioned after a failed migration: %s", ddl)
	}
	if _, err := failed.DB().Exec("SELECT 1 FROM vec_migrate_tmp LIMIT 1"); err == nil {
		t.Fatal("staging table must not survive a failed migration")
	}
	if err := failed.Close(); err != nil {
		t.Fatalf("close failed store: %v", err)
	}

	// The next open retries and completes it.
	s2 := openTestStoreAt(t, dbPath)
	if v := storedSchemaVersion(t, s2); v != schemaVersion {
		t.Fatalf("schema_version=%d after retried migration, want %d", v, schemaVersion)
	}
	if ddl := vecTableDDL(t, s2); !strings.Contains(ddl, "source TEXT partition key") {
		t.Fatalf("vec_embeddings not partitioned after retry: %s", ddl)
	}
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings after retry: %v", err)
	}
	if vecCount != wantRows {
		t.Fatalf("vec_embeddings count=%d after retry, want %d", vecCount, wantRows)
	}
}

func TestVecPartitionMigrationWaitsForAConcurrentWriter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6-busy.db")

	s := openTestStoreAt(t, dbPath)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
	wantRows := seedVecFixture(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	unpartitionVecTable(t, dbPath, 3, 6)

	// Another poisk process indexing when this one starts must not cost the
	// migration. SQLite refuses a read-to-write upgrade at once, without
	// consulting the busy handler, so the migration has to take the write lock
	// before it reads anything.
	blocker, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open blocking connection: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatalf("begin blocking tx: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO embedding_files (source, file_path, mtime) VALUES ('src-c', 'busy.go', 1)"); err != nil {
		t.Fatalf("take the write lock: %v", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = tx.Commit()
	}()

	s2 := openTestStoreAt(t, dbPath)
	if !s2.VecAvailable() {
		t.Fatal("vector search off after a migration that met a concurrent writer")
	}
	if v := storedSchemaVersion(t, s2); v != schemaVersion {
		t.Fatalf("schema_version=%d, want %d", v, schemaVersion)
	}
	if ddl := vecTableDDL(t, s2); !strings.Contains(ddl, "source TEXT partition key") {
		t.Fatalf("vec_embeddings not partitioned: %s", ddl)
	}
	var vecCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount != wantRows {
		t.Fatalf("vec_embeddings count=%d, want %d", vecCount, wantRows)
	}
}

func TestVecPartitionMigrationSkipsAnAlreadyPartitionedTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v7-skip.db")

	s := openTestStoreAt(t, dbPath)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
	seedVecFixture(t, s)

	// An earlier deferred step holds the stored version below 7, so the vec step
	// is planned again on a table that already has the partition key. Copying
	// every vector a second time costs ~30s on a real index, and only the staged
	// copy's JOIN drops an orphan vector, so an orphan that survives proves the
	// migration did no work.
	if _, err := s.DB().Exec(
		"INSERT INTO vec_embeddings (rowid, source, embedding) VALUES (9999, 'src-a', "+s.VecValueExpr()+")",
		Float32sToBlob([]float32{0, 0, 1}),
	); err != nil {
		t.Fatalf("insert orphan vec row: %v", err)
	}

	if err := s.runVecPartitionMigration(); err != nil {
		t.Fatalf("migration on an already partitioned table: %v", err)
	}

	var orphans int
	if err := s.DB().QueryRow(
		"SELECT COUNT(*) FROM vec_embeddings v LEFT JOIN embeddings e ON e.id = v.rowid WHERE e.id IS NULL",
	).Scan(&orphans); err != nil {
		t.Fatalf("count orphan vectors: %v", err)
	}
	if orphans != 1 {
		t.Fatalf("orphan vectors=%d, want 1: the migration re-ran on a partitioned table", orphans)
	}
}

func TestMigrationPlanReachedVersion(t *testing.T) {
	tests := []struct {
		name          string
		storedVersion int
		ftsReady      bool
		vecReady      bool
		want          int
	}{
		{name: "v6 vec step succeeds", storedVersion: 6, vecReady: true, want: 7},
		{name: "v6 vec step fails", storedVersion: 6, vecReady: false, want: 6},
		{name: "v5 both steps succeed", storedVersion: 5, ftsReady: true, vecReady: true, want: 7},
		{name: "v5 fts succeeds, vec fails", storedVersion: 5, ftsReady: true, vecReady: false, want: 6},
		{name: "v5 fts fails, vec succeeds anyway", storedVersion: 5, ftsReady: false, vecReady: true, want: 5},
		{name: "v5 both fail", storedVersion: 5, want: 5},
		{name: "already current", storedVersion: 7, ftsReady: true, vecReady: true, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planMigration(tt.storedVersion)
			if got := plan.reachedVersion(tt.storedVersion, tt.ftsReady, tt.vecReady); got != tt.want {
				t.Fatalf("reachedVersion(stored=%d, fts=%v, vec=%v) = %d, want %d",
					tt.storedVersion, tt.ftsReady, tt.vecReady, got, tt.want)
			}
		})
	}
}

func TestPlanMigration(t *testing.T) {
	tests := []struct {
		name          string
		storedVersion int
		want          migrationPlan
	}{
		{"current version needs nothing", 7, migrationPlan{}},
		{"v6 repartitions vectors", 6, migrationPlan{vecPart: true}},
		{"v5 chains fts then vectors", 5, migrationPlan{fts: true, vecPart: true}},
		{"v4 has no path", 4, migrationPlan{rebuild: true}},
		{"future version rebuilds", 99, migrationPlan{rebuild: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planMigration(tt.storedVersion); got != tt.want {
				t.Fatalf("planMigration(%d) = %+v, want %+v", tt.storedVersion, got, tt.want)
			}
		})
	}
}
