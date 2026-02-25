package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	return openTestStoreAt(t, dbPath)
}

func openTestStoreAt(t *testing.T, dbPath string) *Store {
	t.Helper()
	s, err := Open(dbPath, 3)
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

	changed, _ := s.ModelChanged("src", "model-v1", 768)
	if !changed {
		t.Fatal("expected changed for new source")
	}

	if err := s.UpdateMeta("src", "model-v1", 768); err != nil {
		t.Fatal(err)
	}
	changed, _ = s.ModelChanged("src", "model-v1", 768)
	if changed {
		t.Fatal("expected not changed")
	}

	changed, _ = s.ModelChanged("src", "model-v2", 768)
	if !changed {
		t.Fatal("expected changed after model change")
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
	if err := s.UpdateMeta("src", "model", 3); err != nil {
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
	changed, _ := s.ModelChanged("src", "model", 3)
	if !changed {
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

	if err := s.UpdateMeta("src", "model", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMeta("docs", "model", 3); err != nil {
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
	s, err := Open(dbPath, 3)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal("db file not created")
	}
}

func TestSchemaVersionMigrationDropsOldData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrate.db")

	s := openTestStoreAt(t, dbPath)
	if err := s.InsertEntries("src", "main.go", []Entry{
		{LineNum: 1, EndLine: 1, Text: "func old()", Embedding: []float32{1, 0, 0}, Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "old"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_version SET version = ?", schemaVersion-1); err != nil {
		db.Close()
		t.Fatalf("downgrade schema_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	s2 := openTestStoreAt(t, dbPath)
	count, err := s2.Count("src")
	if err != nil {
		t.Fatalf("count after migration: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected full reindex reset (count=0), got %d", count)
	}

	var version int
	if err := s2.DB().QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema_version=%d, want %d", version, schemaVersion)
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
	if err := s.UpdateMeta("src", "model", 3); err != nil {
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
	changed, _ := s.ModelChanged("src", "model", 3)
	if !changed {
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
