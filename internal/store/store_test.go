package store

import (
	"os"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
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
