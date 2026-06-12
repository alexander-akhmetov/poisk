package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/search"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// newBrokenEmbedServer returns an httptest.Server that always responds 500.
func newBrokenEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestStackWithEmbedURL is like newTestStack but lets the caller supply
// a custom embedding server URL (useful for broken-server tests).
func newTestStackWithEmbedURL(t *testing.T, embedURL string, dims int) *testStack {
	t.Helper()

	corpus, _ := filepath.EvalSymlinks(t.TempDir())

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedURL
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
	db, err := store.Open(dbPath, dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	client := embed.NewClient(embedURL, "", cfg.Embedding.Model, dims, false, false)
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

func TestSearchPartialVecFailure(t *testing.T) {
	// Use a working embed server for indexing, then swap to a broken one for search.
	dims := 3
	goodSrv := testEmbedServer(t, dims)
	t.Cleanup(goodSrv.Close)

	ts := newTestStackWithEmbedURL(t, goodSrv.URL, dims)

	writeFixture(t, ts.Corpus, "algo.go", `package algo

// BinarySearch searches sorted slices.
func BinarySearch(data []int, target int) int {
    lo, hi := 0, len(data)-1
    for lo <= hi {
        mid := lo + (hi-lo)/2
        if data[mid] == target { return mid }
    }
    return -1
}
`)
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Now create a searcher that points to a broken embed server.
	brokenSrv := newBrokenEmbedServer(t)
	brokenClient := embed.NewClient(brokenSrv.URL, "", "test-model", dims, false, false)
	brokenSearcher := search.NewSearcher(ts.DB, brokenClient, ts.Cfg, nil)

	// Hybrid search: vec will fail, but FTS should still produce results.
	results, err := brokenSearcher.Search(context.Background(), "BinarySearch", 5, nil)
	if len(results) == 0 {
		t.Fatalf("expected FTS results despite vec failure, err=%v", err)
	}
	// err should be non-nil (partial failure reported).
	if err == nil {
		t.Error("expected partial error from vec failure")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	ts := newTestStack(t)

	writeFixture(t, ts.Corpus, "hello.go", "package hello\n\nfunc Hello() string { return \"hello\" }\n")
	if _, err := ts.Indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	tests := []struct {
		name  string
		query string
	}{
		{"empty", ""},
		{"whitespace", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ts.Searcher.Search(context.Background(), tt.query, 5, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != 0 {
				t.Fatalf("expected 0 results for %q, got %d", tt.query, len(results))
			}
		})
	}
}

func TestSearchContextAnnotation(t *testing.T) {
	dims := 3
	embedSrv := testEmbedServer(t, dims)
	t.Cleanup(embedSrv.Close)

	corpus, _ := filepath.EvalSymlinks(t.TempDir())

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "test-model"
	cfg.Embedding.Dimensions = dims
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Folders = []config.FolderConfig{
		{
			Path:        corpus,
			Description: "test corpus",
			Context:     map[string]string{"src": "source code", "docs": "documentation"},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	client := embed.NewClient(embedSrv.URL, "", cfg.Embedding.Model, dims, false, false)
	indexer := index.NewIndexer(db, client, &cfg)
	searcher := search.NewSearcher(db, client, &cfg, nil)

	writeFixture(t, corpus, "src/handler.go", `package src

// HandleRequest processes incoming requests.
func HandleRequest() string { return "handled" }
`)
	if _, err := indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	// FTS-only search to avoid vec dependency on test.
	results, err := searcher.Search(context.Background(), "lex:HandleRequest", 5, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for HandleRequest")
	}

	found := false
	for _, r := range results {
		if len(r.Context) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one result with context annotation from folder config")
	}
}
