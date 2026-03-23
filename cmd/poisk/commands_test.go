package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/search"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// testEmbedServer returns an httptest.Server that produces deterministic
// embeddings (unit vector along first dimension).
func testEmbedServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		resp := struct {
			Data []datum `json:"data"`
		}{}
		for i := range req.Input {
			vec := make([]float32, dims)
			vec[0] = 1.0
			resp.Data = append(resp.Data, datum{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

type testStack struct {
	DB       *store.Store
	Indexer  *index.Indexer
	Searcher *search.Searcher
	Cfg      *config.Config
	Corpus   string
}

// newTestStack wires up the same dependency graph as the CLI commands but
// pointing at temp directories and a mock embedding server.
func newTestStack(t *testing.T) *testStack {
	t.Helper()

	dims := 3
	corpus, _ := filepath.EvalSymlinks(t.TempDir())
	embedSrv := testEmbedServer(t, dims)
	t.Cleanup(embedSrv.Close)

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "test-model"
	cfg.Embedding.Dimensions = dims
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Folders = []config.FolderConfig{
		{Path: corpus, Description: "test corpus"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, dims)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, dims, false)
	indexer := index.NewIndexer(db, client, &cfg)
	searcher := search.NewSearcher(db, client, &cfg, nil)

	return &testStack{
		DB:       db,
		Indexer:  indexer,
		Searcher: searcher,
		Cfg:      &cfg,
		Corpus:   corpus,
	}
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
	mt := time.Now()
	_ = os.Chtimes(path, mt, mt)
	return path
}

// TestStatusJSONOutputStructure verifies that the --json status output has the
// expected shape — this is the contract that scripts and skills depend on.
func TestStatusJSONOutputStructure(t *testing.T) {
	ts := newTestStack(t)

	writeFixture(t, ts.Corpus, "hello.go", "package main\n\nfunc main() { println(\"hello world from test fixture\") }\n")
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Build status JSON the same way printStatusJSON does.
	status := struct {
		Folders      []FolderStatusJSON `json:"folders"`
		VecAvailable bool               `json:"vec_available"`
		FTSAvailable bool               `json:"fts_available"`
	}{
		VecAvailable: ts.DB.VecAvailable(),
		FTSAvailable: ts.DB.FTSAvailable(),
	}
	for _, f := range ts.Cfg.Folders {
		count, _ := ts.DB.Count(f.Path)
		fileCount, _ := ts.DB.TrackedFileCount(f.Path)
		status.Folders = append(status.Folders, FolderStatusJSON{
			Path:        f.Path,
			Description: f.Description,
			Files:       fileCount,
			Chunks:      count,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(status); err != nil {
		t.Fatalf("encode status: %v", err)
	}

	// Decode back and verify structure
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode status JSON: %v", err)
	}

	// Required top-level keys
	for _, key := range []string{"folders", "vec_available", "fts_available"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing required key %q in status output", key)
		}
	}

	folders, ok := decoded["folders"].([]any)
	if !ok || len(folders) == 0 {
		t.Fatal("expected at least one folder in status output")
	}
	folder := folders[0].(map[string]any)
	for _, key := range []string{"path", "description", "files", "chunks"} {
		if _, ok := folder[key]; !ok {
			t.Errorf("missing required key %q in folder status", key)
		}
	}

	// After indexing, there should be tracked files and chunks
	if folder["files"].(float64) < 1 {
		t.Errorf("expected at least 1 tracked file, got %v", folder["files"])
	}
	if folder["chunks"].(float64) < 1 {
		t.Errorf("expected at least 1 chunk, got %v", folder["chunks"])
	}
}

// TestIndexAndSearchRoundTrip verifies that indexing fixtures and then
// searching for them returns results — the core user-visible behavior.
func TestIndexAndSearchRoundTrip(t *testing.T) {
	ts := newTestStack(t)

	writeFixture(t, ts.Corpus, "algo.go", `package algo

// BinarySearch finds target in sorted slice.
func BinarySearch(data []int, target int) int {
    lo, hi := 0, len(data)-1
    for lo <= hi {
        mid := lo + (hi-lo)/2
        if data[mid] == target {
            return mid
        } else if data[mid] < target {
            lo = mid + 1
        } else {
            hi = mid - 1
        }
    }
    return -1
}
`)
	writeFixture(t, ts.Corpus, "README.md", "# Algorithm Library\n\nThis package provides common algorithms including binary search.\n")

	stats, err := ts.Indexer.IndexAll(context.Background())
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	totalFiles := 0
	totalChunks := 0
	for _, s := range stats {
		totalFiles += s.FilesProcessed
		totalChunks += s.ChunksCreated
	}
	if totalFiles < 2 {
		t.Errorf("expected at least 2 files indexed, got %d", totalFiles)
	}
	if totalChunks < 1 {
		t.Errorf("expected at least 1 chunk, got %d", totalChunks)
	}

	// FTS-only search (no vec needed for basic regression check)
	results, err := ts.Searcher.Search(context.Background(), "lex:BinarySearch", 5, nil)
	if err != nil && len(results) == 0 {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results for 'BinarySearch'")
	}
}

// TestSearchNoResults verifies the "no results" path produces an empty
// slice instead of an error.
func TestSearchNoResults(t *testing.T) {
	ts := newTestStack(t)

	writeFixture(t, ts.Corpus, "simple.txt", "this file has some simple content for testing purposes only")
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Search for something that doesn't exist in the corpus
	results, err := ts.Searcher.Search(context.Background(), "lex:xyznonexistenttermzzz", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestIndexIncrementalUpdate verifies that re-indexing after file changes
// picks up the new content — the incremental indexing contract.
func TestIndexIncrementalUpdate(t *testing.T) {
	ts := newTestStack(t)

	path := writeFixture(t, ts.Corpus, "data.txt", "original content that is long enough to be indexed as a chunk")
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count1, _ := ts.DB.Count(ts.Corpus)
	if count1 == 0 {
		t.Fatal("expected chunks after first index")
	}

	// Update file with new content
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("updated content that is completely different and long enough to be indexed"), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now()
	_ = os.Chtimes(path, mt, mt)

	stats, err := ts.Indexer.IndexAll(context.Background())
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats[0].FilesProcessed == 0 {
		t.Error("expected file to be re-indexed after mtime change")
	}

	// Verify the new content is searchable via FTS
	results, _ := ts.Searcher.Search(context.Background(), "lex:updated", 5, nil)
	if len(results) == 0 {
		t.Error("expected to find updated content")
	}
}

// TestIndexDeletedFileRemoved verifies that deleting a file and re-indexing
// removes its chunks from the store.
func TestIndexDeletedFileRemoved(t *testing.T) {
	ts := newTestStack(t)

	path := writeFixture(t, ts.Corpus, "ephemeral.txt", "ephemeral content that is long enough to produce indexed chunks")
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	count, _ := ts.DB.Count(ts.Corpus)
	if count == 0 {
		t.Fatal("expected chunks after index")
	}

	os.Remove(path)
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("re-index: %v", err)
	}

	count, _ = ts.DB.Count(ts.Corpus)
	if count != 0 {
		t.Fatalf("expected 0 chunks after file deletion, got %d", count)
	}
}
