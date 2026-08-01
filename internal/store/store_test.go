package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	return openTestStoreAt(t, dbPath)
}

func openTestStoreAt(t *testing.T, dbPath string) *Store {
	t.Helper()
	s, err := Open(dbPath, 3, QuantizationInt8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreOpenClose(t *testing.T) {
	s := openTestStore(t)
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}
}

func TestFileMtimeCRUD(t *testing.T) {
	s := openTestStore(t)

	// Get unknown
	_, ok, err := s.GetFileMtime("src", "test.go")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected not found")
	}

	// Set
	if err := s.SetFileMtime("src", "test.go", 12345); err != nil {
		t.Fatal(err)
	}
	mt, ok, err := s.GetFileMtime("src", "test.go")
	if err != nil || !ok || mt != 12345 {
		t.Fatalf("got mtime=%d, ok=%v, err=%v", mt, ok, err)
	}

	// Update
	if err := s.SetFileMtime("src", "test.go", 67890); err != nil {
		t.Fatal(err)
	}
	mt, _, _ = s.GetFileMtime("src", "test.go")
	if mt != 67890 {
		t.Fatalf("got mtime=%d, want 67890", mt)
	}

	// Delete
	if err := s.DeleteFile("src", "test.go"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = s.GetFileMtime("src", "test.go")
	if ok {
		t.Fatal("expected deleted")
	}
}

func TestTrackedFiles(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetFileMtime("src", "a.go", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileMtime("src", "b.go", 200); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileMtime("other", "c.go", 300); err != nil {
		t.Fatal(err)
	}

	m, err := s.TrackedFiles("src")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("got %d files, want 2", len(m))
	}
	if m["a.go"] != 100 || m["b.go"] != 200 {
		t.Fatalf("unexpected mtimes: %v", m)
	}
}

func TestInsertAndCount(t *testing.T) {
	s := openTestStore(t)

	entries := []Entry{
		{LineNum: 1, Text: "hello", Embedding: []float32{1, 0, 0}},
		{LineNum: 5, Text: "world", Embedding: []float32{0, 1, 0}},
	}
	if err := s.InsertEntries("src", "test.go", entries); err != nil {
		t.Fatal(err)
	}

	count, _ := s.Count("src")
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	// Re-insert replaces
	if err := s.InsertEntries("src", "test.go", entries[:1]); err != nil {
		t.Fatal(err)
	}
	count, _ = s.Count("src")
	if count != 1 {
		t.Fatalf("after re-insert count = %d, want 1", count)
	}
}

func TestModelChanged(t *testing.T) {
	s := openTestStore(t)

	mc, _ := s.ModelChanged("src", "model-v1", 768, QuantizationInt8)
	if !mc.Changed {
		t.Fatal("expected changed for new source")
	}

	if err := s.UpdateMeta("src", "model-v1", 768, QuantizationInt8); err != nil {
		t.Fatal(err)
	}
	mc, _ = s.ModelChanged("src", "model-v1", 768, QuantizationInt8)
	if mc.Changed {
		t.Fatal("expected not changed")
	}

	mc, _ = s.ModelChanged("src", "model-v2", 768, QuantizationInt8)
	if !mc.Changed {
		t.Fatal("expected changed after model change")
	}
	if mc.OldModel != "model-v1" || mc.OldDims != 768 {
		t.Fatalf("expected old model=model-v1 dims=768, got model=%s dims=%d", mc.OldModel, mc.OldDims)
	}

	mc, _ = s.ModelChanged("src", "model-v1", 768, QuantizationFloat32)
	if !mc.Changed {
		t.Fatal("expected changed after quantization change")
	}
	if mc.OldQuantization != QuantizationInt8 {
		t.Fatalf("expected old quantization=int8, got %q", mc.OldQuantization)
	}
}

func TestClearSource(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetFileMtime("src", "f.go", 111); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries("src", "f.go", []Entry{
		{LineNum: 1, Text: "test", Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMeta("src", "model", 3, QuantizationInt8); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearSource("src"); err != nil {
		t.Fatal(err)
	}

	count, _ := s.Count("src")
	if count != 0 {
		t.Fatal("expected 0 entries")
	}
	_, ok, _ := s.GetFileMtime("src", "f.go")
	if ok {
		t.Fatal("expected mtime cleared")
	}
	mc, _ := s.ModelChanged("src", "model", 3, QuantizationInt8)
	if !mc.Changed {
		t.Fatal("expected meta cleared")
	}
}

func TestAllSources(t *testing.T) {
	s := openTestStore(t)

	sources, err := s.AllSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(sources))
	}

	if err := s.UpdateMeta("src", "model", 3, QuantizationInt8); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMeta("docs", "model", 3, QuantizationInt8); err != nil {
		t.Fatal(err)
	}

	sources, err = s.AllSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}

	got := make(map[string]bool)
	for _, src := range sources {
		got[src] = true
	}
	if !got["src"] || !got["docs"] {
		t.Fatalf("unexpected sources: %v", sources)
	}
}

func TestGetEntriesByPath(t *testing.T) {
	s := openTestStore(t)

	entries := []Entry{
		{LineNum: 10, EndLine: 15, Text: "func foo()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "foo"},
		{LineNum: 1, EndLine: 5, Text: "package main", Embedding: []float32{0, 1, 0}, Folder: "src", Language: "go", Kind: "package_clause", Symbol: "main"},
		{LineNum: 20, EndLine: 25, Text: "func bar()", Embedding: []float32{0, 0, 1}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "bar"},
	}
	if err := s.InsertEntries("src", "main.go", entries); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetEntriesByPath("src", "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	// Should be ordered by line_num
	if got[0].LineNum != 1 || got[1].LineNum != 10 || got[2].LineNum != 20 {
		t.Fatalf("unexpected order: %d, %d, %d", got[0].LineNum, got[1].LineNum, got[2].LineNum)
	}
	// Embedding should be nil (not hydrated)
	for i, e := range got {
		if e.Embedding != nil {
			t.Fatalf("entry %d: expected nil embedding", i)
		}
	}
	// Metadata should be preserved
	if got[0].Symbol != "main" || got[1].Symbol != "foo" || got[2].Symbol != "bar" {
		t.Fatalf("symbols: %q, %q, %q", got[0].Symbol, got[1].Symbol, got[2].Symbol)
	}

	// Different source returns empty
	got, err = s.GetEntriesByPath("other", "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for other source, got %d", len(got))
	}
}

func TestTrackedFilePaths(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetFileMtime("src", "a.go", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileMtime("src", "b.go", 200); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileMtime("other", "c.go", 300); err != nil {
		t.Fatal(err)
	}

	paths, err := s.TrackedFilePaths("src")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(paths))
	}
	// Should be sorted
	if paths[0] != "a.go" || paths[1] != "b.go" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDBPathCreation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "nested", "test.db")
	s, err := Open(dbPath, 3, QuantizationInt8)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal("db file not created")
	}
}

func TestSchemaVersionMigration(t *testing.T) {
	// The chunks_fts layout before v6: own content storage, id column.
	const v5FTSDDL = `CREATE VIRTUAL TABLE chunks_fts USING fts5(chunk_text, id UNINDEXED, source UNINDEXED, file_path UNINDEXED, line_num UNINDEXED, folder UNINDEXED, end_line UNINDEXED, language, chunk_kind, symbol)`

	tests := []struct {
		name          string
		storedVersion int
		wantCount     int // surviving embeddings/vec/fts rows after migration
	}{
		{
			name:          "version with no migration path drops all data",
			storedVersion: 3,
			wantCount:     0,
		},
		{
			name:          "v5 chains through both targeted migrations and keeps data",
			storedVersion: 5,
			wantCount:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "migrate.db")

			s := openTestStoreAt(t, dbPath)
			ftsAvailable := s.FTSAvailable()
			vecAvailable := s.VecAvailable()
			if err := s.SetFileMtime("src", "main.go", 12345); err != nil {
				t.Fatal(err)
			}
			if err := s.InsertEntries("src", "main.go", []Entry{
				{LineNum: 1, EndLine: 1, Text: "func old()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "old"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("close store: %v", err)
			}

			// Recreate vec_embeddings without the source partition key, so a
			// stored version below 7 meets the real pre-v7 shape.
			if vecAvailable {
				unpartitionVecTable(t, dbPath, 3, tt.storedVersion)
			}

			db, err := sql.Open("sqlite3", dbPath)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			if _, err := db.Exec("UPDATE schema_version SET version = ?", tt.storedVersion); err != nil {
				db.Close()
				t.Fatalf("downgrade schema_version: %v", err)
			}
			// Recreate chunks_fts with the pre-v6 layout so the migration runs
			// against the real old shape.
			if ftsAvailable {
				if _, err := db.Exec("DROP TABLE chunks_fts"); err != nil {
					db.Close()
					t.Fatalf("drop chunks_fts: %v", err)
				}
				if _, err := db.Exec(v5FTSDDL); err != nil {
					db.Close()
					t.Fatalf("create v5 chunks_fts: %v", err)
				}
				if _, err := db.Exec(`INSERT INTO chunks_fts(chunk_text, id, source, file_path, line_num, folder, end_line, language, chunk_kind, symbol)
					SELECT chunk_text, CAST(id AS TEXT), source, file_path, CAST(line_num AS TEXT), COALESCE(folder, ''), CAST(end_line AS TEXT), language, chunk_kind, symbol
					FROM embeddings`); err != nil {
					db.Close()
					t.Fatalf("populate v5 chunks_fts: %v", err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}

			s2 := openTestStoreAt(t, dbPath)
			count, err := s2.Count("src")
			if err != nil {
				t.Fatalf("count after migration: %v", err)
			}
			if count != tt.wantCount {
				t.Fatalf("embeddings count=%d, want %d", count, tt.wantCount)
			}

			if s2.VecAvailable() {
				var vecCount int
				if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
					t.Fatalf("count vec_embeddings: %v", err)
				}
				if vecCount != tt.wantCount {
					t.Fatalf("vec_embeddings count=%d, want %d", vecCount, tt.wantCount)
				}
				// Every path reaches v7, so the vector table carries the
				// partition key whether it was rebuilt or migrated in place.
				if ddl := vecTableDDL(t, s2); !strings.Contains(ddl, "source TEXT partition key") {
					t.Fatalf("vec_embeddings not partitioned after migration: %s", ddl)
				}
			}

			_, ok, err := s2.GetFileMtime("src", "main.go")
			if err != nil {
				t.Fatalf("get mtime after migration: %v", err)
			}
			if wantMtime := tt.wantCount > 0; ok != wantMtime {
				t.Fatalf("mtime present=%v, want %v", ok, wantMtime)
			}

			if s2.FTSAvailable() {
				var ftsCount int
				if err := s2.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'old'").Scan(&ftsCount); err != nil {
					t.Fatalf("fts match after migration: %v", err)
				}
				if ftsCount != tt.wantCount {
					t.Fatalf("fts match count=%d, want %d", ftsCount, tt.wantCount)
				}
			}

			var version int
			if err := s2.DB().QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
				t.Fatalf("read schema_version: %v", err)
			}
			if version != schemaVersion {
				t.Fatalf("schema_version=%d, want %d", version, schemaVersion)
			}
		})
	}
}

func TestFTSMigrationDeferredWhenDropFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "deferred.db")

	s := openTestStoreAt(t, dbPath)
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}
	if err := s.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "func old()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "old"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A build without FTS5 cannot drop the old fts5 virtual table because its
	// module is missing. A view of the same name makes DROP TABLE fail the
	// same way inside an FTS5-enabled test binary.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_version SET version = 5"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE chunks_fts"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE VIEW chunks_fts AS SELECT 1 AS chunk_text"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Open must succeed despite the failed drop, keeping the version at 5 and
	// the data intact so a later FTS5-enabled run can retry the migration.
	s2 := openTestStoreAt(t, dbPath)
	var version int
	if err := s2.DB().QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("schema_version=%d after deferred migration, want 5", version)
	}
	count, err := s2.Count("src")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("embeddings count=%d, want 1", count)
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}

	// Once the old table can be dropped, the retried migration completes.
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP VIEW chunks_fts"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s3 := openTestStoreAt(t, dbPath)
	if err := s3.DB().QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema_version=%d after retried migration, want %d", version, schemaVersion)
	}
	var ftsCount int
	if err := s3.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'old'").Scan(&ftsCount); err != nil {
		t.Fatal(err)
	}
	if ftsCount != 1 {
		t.Fatalf("fts match count=%d after retried migration, want 1", ftsCount)
	}
}

func TestReopenWithSameVersionDoesNotDropData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reopen.db")

	s1 := openTestStoreAt(t, dbPath)
	if err := s1.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "func keep()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "keep"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s1.UpdateMeta("src", "model-v1", 3, QuantizationInt8); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close s1: %v", err)
	}

	// Reopen with the same schema version — data and meta must survive.
	s2 := openTestStoreAt(t, dbPath)

	count, err := s2.Count("src")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count=%d after reopen, want 1 (data was dropped)", count)
	}

	mc, err := s2.ModelChanged("src", "model-v1", 3, QuantizationInt8)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if mc.Changed {
		t.Fatal("ModelChanged=true after reopen with same version; embedding_meta was dropped")
	}
}

func TestIdxEmbMetaExists(t *testing.T) {
	s := openTestStore(t)

	var count int
	err := s.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_emb_meta'",
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("expected idx_emb_meta index to exist")
	}
}

func TestClearSourceAtomicity(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetFileMtime("src", "a.go", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries("src", "a.go", []Entry{
		{LineNum: 1, Text: "func A()", Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMeta("src", "model", 3, QuantizationInt8); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearSource("src"); err != nil {
		t.Fatal(err)
	}

	count, _ := s.Count("src")
	if count != 0 {
		t.Fatal("embeddings not cleared")
	}
	_, ok, _ := s.GetFileMtime("src", "a.go")
	if ok {
		t.Fatal("embedding_files not cleared")
	}
	mc, _ := s.ModelChanged("src", "model", 3, QuantizationInt8)
	if !mc.Changed {
		t.Fatal("embedding_meta not cleared")
	}

	if s.VecAvailable() {
		var vecCount int
		_ = s.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount)
		if vecCount != 0 {
			t.Fatal("vec_embeddings not cleared")
		}
	}
	if s.FTSAvailable() {
		var ftsCount int
		_ = s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE source = 'src'").Scan(&ftsCount)
		if ftsCount != 0 {
			t.Fatal("chunks_fts not cleared")
		}
	}
}

func TestDeleteFileAtomicity(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetFileMtime("src", "a.go", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileMtime("src", "b.go", 200); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries("src", "a.go", []Entry{
		{LineNum: 1, Text: "func A()", Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries("src", "b.go", []Entry{
		{LineNum: 1, Text: "func B()", Embedding: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteFile("src", "a.go"); err != nil {
		t.Fatal(err)
	}

	// a.go should be gone
	count, _ := s.Count("src")
	if count != 1 {
		t.Fatalf("expected 1 remaining entry, got %d", count)
	}
	_, ok, _ := s.GetFileMtime("src", "a.go")
	if ok {
		t.Fatal("a.go mtime not deleted")
	}
	// b.go should remain
	_, ok, _ = s.GetFileMtime("src", "b.go")
	if !ok {
		t.Fatal("b.go mtime should still exist")
	}

	if s.VecAvailable() {
		var vecCount int
		_ = s.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount)
		if vecCount != 1 {
			t.Fatalf("expected 1 vec_embedding, got %d", vecCount)
		}
	}
	if s.FTSAvailable() {
		var ftsCount int
		_ = s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE source = 'src' AND file_path = 'a.go'").Scan(&ftsCount)
		if ftsCount != 0 {
			t.Fatal("chunks_fts for a.go not cleared")
		}
	}
}

func TestInsertAndGetChunks(t *testing.T) {
	s := openTestStore(t)

	chunks := []domain.ChunkWithEmbedding{
		{
			Chunk: domain.Chunk{
				Source:   "src",
				FilePath: "main.go",
				LineNum:  1,
				EndLine:  5,
				Text:     "package main",
				Folder:   "src",
				Language: "go",
				Kind:     "package_clause",
				Symbol:   "main",
			},
			Embedding: []float32{1, 0, 0},
		},
		{
			Chunk: domain.Chunk{
				Source:   "src",
				FilePath: "main.go",
				LineNum:  10,
				EndLine:  20,
				Text:     "func hello() {}",
				Folder:   "src",
				Language: "go",
				Kind:     "function_declaration",
				Symbol:   "hello",
			},
			Embedding: []float32{0, 1, 0},
		},
	}

	if err := s.InsertChunks("src", "main.go", chunks); err != nil {
		t.Fatalf("InsertChunks: %v", err)
	}

	got, err := s.GetChunksByPath("src", "main.go")
	if err != nil {
		t.Fatalf("GetChunksByPath: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}

	// Ordered by line_num
	if got[0].LineNum != 1 || got[1].LineNum != 10 {
		t.Fatalf("order: lines %d, %d; want 1, 10", got[0].LineNum, got[1].LineNum)
	}

	// Metadata roundtrip
	if got[0].Symbol != "main" || got[0].Language != "go" || got[0].Kind != "package_clause" {
		t.Errorf("chunk 0 metadata: symbol=%q lang=%q kind=%q", got[0].Symbol, got[0].Language, got[0].Kind)
	}
	if got[1].Symbol != "hello" || got[1].Kind != "function_declaration" {
		t.Errorf("chunk 1 metadata: symbol=%q kind=%q", got[1].Symbol, got[1].Kind)
	}

	// Different source returns empty
	empty, err := s.GetChunksByPath("other", "main.go")
	if err != nil {
		t.Fatalf("GetChunksByPath other: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 chunks for other source, got %d", len(empty))
	}
}

func TestTrackedFileCount(t *testing.T) {
	s := openTestStore(t)

	// Empty source
	count, err := s.TrackedFileCount("src")
	if err != nil {
		t.Fatalf("count empty: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty count = %d, want 0", count)
	}

	// Add files
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if err := s.SetFileMtime("src", f, 100); err != nil {
			t.Fatalf("SetFileMtime %s: %v", f, err)
		}
	}
	if err := s.SetFileMtime("other", "d.go", 200); err != nil {
		t.Fatal(err)
	}

	count, err = s.TrackedFileCount("src")
	if err != nil {
		t.Fatalf("count src: %v", err)
	}
	if count != 3 {
		t.Fatalf("src count = %d, want 3", count)
	}

	count, err = s.TrackedFileCount("other")
	if err != nil {
		t.Fatalf("count other: %v", err)
	}
	if count != 1 {
		t.Fatalf("other count = %d, want 1", count)
	}
}

func TestVec0DimensionChange(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dims.db")

	// Open with dims=3, insert data, close
	s1, err := Open(dbPath, 3, QuantizationInt8)
	if err != nil {
		t.Fatalf("open dims=3: %v", err)
	}
	if !s1.VecAvailable() {
		s1.Close()
		t.Skip("vec0 not available")
	}
	if err := s1.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 5, Text: "func A()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "A"},
	}); err != nil {
		t.Fatalf("insert dims=3: %v", err)
	}
	if err := s1.UpdateMeta("src", "model-v1", 3, QuantizationInt8); err != nil {
		t.Fatalf("update meta dims=3: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close dims=3: %v", err)
	}

	// Reopen with dims=5 — vec0 table should be recreated
	s2, err := Open(dbPath, 5, QuantizationInt8)
	if err != nil {
		t.Fatalf("open dims=5: %v", err)
	}
	defer s2.Close()

	if !s2.VecAvailable() {
		t.Fatal("expected vec0 available after dimension change")
	}

	// Old vec data should be gone (vec0 table was dropped and recreated)
	var vecCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount != 0 {
		t.Fatalf("expected 0 vec entries after dimension change, got %d", vecCount)
	}

	// Stale meta with old dimensions should be cleaned
	mc, err := s2.ModelChanged("src", "model-v1", 5, QuantizationInt8)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if !mc.Changed {
		t.Fatal("expected model changed after dimension increase")
	}
}

func TestVec0QuantizationChange(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "quant.db")

	// Open with int8, insert data, close
	s1, err := Open(dbPath, 3, QuantizationInt8)
	if err != nil {
		t.Fatalf("open int8: %v", err)
	}
	if !s1.VecAvailable() {
		s1.Close()
		t.Skip("vec0 not available")
	}
	if err := s1.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 5, Text: "func A()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "A"},
	}); err != nil {
		t.Fatalf("insert int8: %v", err)
	}
	if err := s1.UpdateMeta("src", "model-v1", 3, QuantizationInt8); err != nil {
		t.Fatalf("update meta int8: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close int8: %v", err)
	}

	// Reopen with float32 — vec0 table should be recreated
	s2, err := Open(dbPath, 3, QuantizationFloat32)
	if err != nil {
		t.Fatalf("open float32: %v", err)
	}
	defer s2.Close()

	if !s2.VecAvailable() {
		t.Fatal("expected vec0 available after quantization change")
	}

	var ddl string
	if err := s2.DB().QueryRow("SELECT sql FROM sqlite_master WHERE name='vec_embeddings'").Scan(&ddl); err != nil {
		t.Fatalf("read vec_embeddings ddl: %v", err)
	}
	if !strings.Contains(ddl, "float[3]") {
		t.Fatalf("expected float[3] vec0 column after quantization change, got: %s", ddl)
	}

	// Old vec data should be gone (vec0 table was dropped and recreated)
	var vecCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount != 0 {
		t.Fatalf("expected 0 vec entries after quantization change, got %d", vecCount)
	}

	// Stale meta with old quantization should be cleaned
	mc, err := s2.ModelChanged("src", "model-v1", 3, QuantizationFloat32)
	if err != nil {
		t.Fatalf("ModelChanged: %v", err)
	}
	if !mc.Changed {
		t.Fatal("expected model changed after quantization change")
	}
}

func TestVec0KNNRoundtrip(t *testing.T) {
	tests := []struct {
		name         string
		quantization string
		wantElem     string
	}{
		{"int8", QuantizationInt8, "int8[3]"},
		{"float32", QuantizationFloat32, "float[3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "knn.db")
			s, err := Open(dbPath, 3, tt.quantization)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { s.Close() })
			if !s.VecAvailable() {
				t.Skip("vec0 not available")
			}

			var ddl string
			if err := s.DB().QueryRow("SELECT sql FROM sqlite_master WHERE name='vec_embeddings'").Scan(&ddl); err != nil {
				t.Fatalf("read vec_embeddings ddl: %v", err)
			}
			if !strings.Contains(ddl, tt.wantElem) || !strings.Contains(ddl, "distance_metric=cosine") {
				t.Fatalf("expected %s cosine vec0 column, got: %s", tt.wantElem, ddl)
			}
			if !strings.Contains(ddl, "source TEXT partition key") {
				t.Fatalf("expected source partition key in vec0 ddl, got: %s", ddl)
			}

			// Unit-norm vectors: exact match, nearby (cos=0.8), orthogonal
			if err := s.InsertEntries("src", "main.go", []Entry{
				{LineNum: 1, Text: "exact", Embedding: []float32{1, 0, 0}},
				{LineNum: 2, Text: "near", Embedding: []float32{0.8, 0.6, 0}},
				{LineNum: 3, Text: "orthogonal", Embedding: []float32{0, 1, 0}},
			}); err != nil {
				t.Fatalf("insert: %v", err)
			}

			queryBlob := Float32sToBlob([]float32{1, 0, 0})
			rows, err := s.DB().Query(
				"SELECT e.chunk_text, v.distance FROM vec_embeddings v JOIN embeddings e ON e.id = v.rowid WHERE v.embedding MATCH "+s.VecValueExpr()+" AND k = 3 ORDER BY v.distance",
				queryBlob,
			)
			if err != nil {
				t.Fatalf("knn query: %v", err)
			}
			defer rows.Close()

			var texts []string
			var distances []float64
			for rows.Next() {
				var text string
				var d float64
				if err := rows.Scan(&text, &d); err != nil {
					t.Fatalf("scan: %v", err)
				}
				texts = append(texts, text)
				distances = append(distances, d)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}

			if len(texts) != 3 {
				t.Fatalf("expected 3 results, got %d", len(texts))
			}
			if texts[0] != "exact" || texts[1] != "near" || texts[2] != "orthogonal" {
				t.Fatalf("unexpected ranking: %v (distances %v)", texts, distances)
			}
			// Cosine distances: exact ~0, near ~0.2, orthogonal ~1
			wantDist := []float64{0, 0.2, 1}
			for i, want := range wantDist {
				if diff := distances[i] - want; diff < -0.05 || diff > 0.05 {
					t.Fatalf("distance[%d] = %v, want ~%v (all: %v)", i, distances[i], want, distances)
				}
			}
		})
	}
}

func TestFTSMetadataColumnsAreIndexedAndSearchable(t *testing.T) {
	s := openTestStore(t)
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	var ddl string
	if err := s.DB().QueryRow("SELECT sql FROM sqlite_master WHERE name='chunks_fts'").Scan(&ddl); err != nil {
		t.Fatalf("read chunks_fts ddl: %v", err)
	}
	if strings.Contains(ddl, "language UNINDEXED") {
		t.Fatalf("language must be indexed in chunks_fts: %s", ddl)
	}
	if strings.Contains(ddl, "chunk_kind UNINDEXED") {
		t.Fatalf("chunk_kind must be indexed in chunks_fts: %s", ddl)
	}
	if strings.Contains(ddl, "symbol UNINDEXED") {
		t.Fatalf("symbol must be indexed in chunks_fts: %s", ddl)
	}
	if !strings.Contains(ddl, "content='embeddings'") || !strings.Contains(ddl, "content_rowid='id'") {
		t.Fatalf("chunks_fts must use external content from embeddings: %s", ddl)
	}

	// External content means no shadow table duplicating chunk_text.
	var contentTables int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='chunks_fts_content'").Scan(&contentTables); err != nil {
		t.Fatal(err)
	}
	if contentTables != 0 {
		t.Fatal("chunks_fts_content shadow table exists; chunk_text is stored twice")
	}

	if err := s.InsertEntries("src", "main.go", []Entry{
		{
			LineNum:   10,
			EndLine:   20,
			Text:      "func FetchUser(id string) string { return id }",
			Embedding: []float32{1, 0, 0},
			Folder:    "src",
			Language:  "go",
			Kind:      "function_declaration",
			Symbol:    "FetchUser",
		},
	}); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		`language:go`,
		`chunk_kind:function_declaration`,
		`symbol:fetchuser`,
	}
	for _, q := range cases {
		var count int
		if err := s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?", q).Scan(&count); err != nil {
			t.Fatalf("fts match %q: %v", q, err)
		}
		if count == 0 {
			t.Fatalf("expected FTS metadata match for query %q", q)
		}
	}
}

func TestInsertEntriesReplacesFTSRows(t *testing.T) {
	s := openTestStore(t)
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 2, Text: "alpha bravo", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "alpha"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 2, Text: "charlie delta", Embedding: []float32{0, 1, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "charlie"},
	}); err != nil {
		t.Fatal(err)
	}

	matchCount := func(q string) int {
		t.Helper()
		var n int
		if err := s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?", q).Scan(&n); err != nil {
			t.Fatalf("fts match %q: %v", q, err)
		}
		return n
	}

	if n := matchCount("bravo"); n != 0 {
		t.Fatalf("stale FTS rows after reindex: match 'bravo' = %d, want 0", n)
	}
	if n := matchCount("charlie"); n != 1 {
		t.Fatalf("match 'charlie' = %d, want 1", n)
	}

	// The full text must still come back through the external content table.
	var text string
	if err := s.DB().QueryRow("SELECT chunk_text FROM chunks_fts WHERE chunks_fts MATCH 'charlie'").Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "charlie delta" {
		t.Fatalf("chunk_text = %q, want %q", text, "charlie delta")
	}

	// COUNT(*) on the FTS table reads from embeddings, so compare the docsize
	// shadow table to catch a desynced index.
	var idxCount, embCount int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts_docsize").Scan(&idxCount); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&embCount); err != nil {
		t.Fatal(err)
	}
	if idxCount != embCount {
		t.Fatalf("fts index rows=%d, embeddings rows=%d; index out of sync", idxCount, embCount)
	}
}

func TestBackfillRebuildsEmptyFTSIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill.db")
	s := openTestStoreAt(t, dbPath)
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "needle haystack", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "needle"},
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a DB written by a build without FTS5: embeddings rows exist but
	// the index is empty.
	if _, err := s.DB().Exec("INSERT INTO chunks_fts(chunks_fts) VALUES('delete-all')"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'needle'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected empty index after delete-all, got %d matches", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTestStoreAt(t, dbPath)
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'needle'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected rebuild to reindex embeddings, got %d matches", n)
	}
}
