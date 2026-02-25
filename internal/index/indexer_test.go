package index

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/store"
)

type testEmbeddingRequest struct {
	Input []string `json:"input"`
}

type testEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func newTestEmbeddingServer(t *testing.T, dims int, failOnCall int32) *httptest.Server {
	t.Helper()

	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if failOnCall > 0 && call >= failOnCall {
			http.Error(w, "embedding failure", http.StatusInternalServerError)
			return
		}

		var req testEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		resp := testEmbeddingResponse{
			Data: make([]struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}, len(req.Input)),
		}
		for i := range req.Input {
			vec := make([]float32, dims)
			vec[0] = 1.0
			resp.Data[i].Embedding = vec
			resp.Data[i].Index = i
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
}

func newTestIndexer(t *testing.T, folder, baseURL string) (*Indexer, *store.Store) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = baseURL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = 3
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: folder, Description: "test"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, cfg.Embedding.Dimensions, false)
	return NewIndexer(db, client, &cfg), db
}

func writeFileWithMtime(t *testing.T, filePath, content string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
	if err := os.Chtimes(filePath, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", filePath, err)
	}
}

func TestIndexFolderClearsEntriesWhenFileProducesNoChunks(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_000, 111)
	writeFileWithMtime(t, filePath, "this line is long enough to be indexed as one chunk", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count == 0 {
		t.Fatal("expected indexed chunks after first index")
	}

	t2 := time.Unix(1_700_000_000, 222)
	writeFileWithMtime(t, filePath, "short", t2)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("second index: %v", err)
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after second index: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected stale chunks to be removed, count=%d", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	if tracked[filePath] != t2.UnixNano() {
		t.Fatalf("tracked mtime=%d, want %d", tracked[filePath], t2.UnixNano())
	}
}

func TestIndexFolderUsesNanosecondMtimeForChangeDetection(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_001, 100)
	writeFileWithMtime(t, filePath, "first-version-token content long enough for indexing", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Same second, different nanoseconds. With second-precision mtimes this change would be missed.
	t2 := time.Unix(1_700_000_001, 900)
	writeFileWithMtime(t, filePath, "second-version-token content long enough for indexing", t2)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("second index: %v", err)
	}

	entries, err := db.GetEntriesByPath(dir, filePath)
	if err != nil {
		t.Fatalf("get entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries after second index")
	}

	foundUpdated := false
	for _, e := range entries {
		if strings.Contains(e.Text, "second-version-token") {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Fatal("expected second content to be indexed after same-second mtime update")
	}
}

func TestIndexFolderRemovesOldDataWhenEmbeddingFails(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	// First embedding call succeeds, second fails.
	server := newTestEmbeddingServer(t, 3, 2)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_002, 100)
	writeFileWithMtime(t, filePath, "initial content long enough for indexing", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count == 0 {
		t.Fatal("expected indexed chunks after first index")
	}

	t2 := time.Unix(1_700_000_002, 200)
	writeFileWithMtime(t, filePath, "updated content still long enough for indexing", t2)
	stats, err := indexer.IndexFolder(context.Background(), dir)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Errors == 0 {
		t.Fatal("expected indexing errors when embedding fails")
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after failed index: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected old chunks removed after embedding failure, count=%d", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	if tracked[filePath] != t1.UnixNano() {
		t.Fatalf("tracked mtime=%d, want previous successful mtime %d", tracked[filePath], t1.UnixNano())
	}
}

func TestIndexAllPrunesRemovedFolders(t *testing.T) {
	dir1, _ := filepath.EvalSymlinks(t.TempDir())
	dir2, _ := filepath.EvalSymlinks(t.TempDir())

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = server.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = 3
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: dir1, Description: "folder1"},
		{Path: dir2, Description: "folder2"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, cfg.Embedding.Dimensions, false)
	indexer := NewIndexer(db, client, &cfg)

	// Write files in both folders
	t1 := time.Unix(1_700_000_000, 100)
	writeFileWithMtime(t, filepath.Join(dir1, "a.txt"), "this line is long enough to be indexed as one chunk in folder one", t1)
	writeFileWithMtime(t, filepath.Join(dir2, "b.txt"), "this line is long enough to be indexed as one chunk in folder two", t1)

	if _, err := indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("first IndexAll: %v", err)
	}

	count1, _ := db.Count(dir1)
	count2, _ := db.Count(dir2)
	if count1 == 0 || count2 == 0 {
		t.Fatalf("expected chunks in both folders, got %d and %d", count1, count2)
	}

	// Remove dir2 from config
	cfg.Folders = []config.FolderConfig{
		{Path: dir1, Description: "folder1"},
	}

	if _, err := indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("second IndexAll: %v", err)
	}

	count1, _ = db.Count(dir1)
	count2, _ = db.Count(dir2)
	if count1 == 0 {
		t.Fatal("expected folder1 chunks to remain")
	}
	if count2 != 0 {
		t.Fatalf("expected folder2 chunks pruned, got %d", count2)
	}

	// Verify meta is cleared too
	changed, _ := db.ModelChanged(dir2, "test-embedding", 3)
	if !changed {
		t.Fatal("expected meta cleared for pruned folder")
	}
}
