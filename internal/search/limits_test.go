package search

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestEffectiveTopK(t *testing.T) {
	tests := []struct {
		name        string
		topK        int
		defaultTopK int
		want        int
	}{
		{name: "unset takes the configured default", topK: 0, defaultTopK: 20, want: 20},
		{name: "negative takes the configured default", topK: -5, defaultTopK: 20, want: 20},
		{name: "ordinary value passes through", topK: 50, defaultTopK: 20, want: 50},
		{name: "at the ceiling", topK: maxTopK, defaultTopK: 20, want: maxTopK},
		{name: "one above the ceiling", topK: maxTopK + 1, defaultTopK: 20, want: maxTopK},
		{name: "far above the ceiling", topK: 5000, defaultTopK: 20, want: maxTopK},
		{name: "oversized default is clamped too", topK: 0, defaultTopK: 9999, want: maxTopK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveTopK(tt.topK, tt.defaultTopK)
			if got != tt.want {
				t.Fatalf("effectiveTopK(%d, %d) = %d, want %d", tt.topK, tt.defaultTopK, got, tt.want)
			}
			if got > maxTopK {
				t.Fatalf("effective topK %d exceeds the ceiling %d", got, maxTopK)
			}
		})
	}
}

func TestFTSFetchLimit(t *testing.T) {
	tests := []struct {
		name string
		topK int
		want int
	}{
		{name: "default over-fetches five to one", topK: 20, want: 100},
		{name: "just below the ceiling", topK: 999, want: 4995},
		{name: "at the topK ceiling", topK: maxTopK, want: maxFTSFetchLimit},
		{name: "an unclamped caller cannot go past it", topK: 5000, want: maxFTSFetchLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ftsFetchLimit(tt.topK)
			if got != tt.want {
				t.Fatalf("ftsFetchLimit(%d) = %d, want %d", tt.topK, got, tt.want)
			}
			if got > maxFTSFetchLimit {
				t.Fatalf("fetch limit %d exceeds the ceiling %d", got, maxFTSFetchLimit)
			}
		})
	}

	if maxFTSFetchLimit != 5000 {
		t.Fatalf("maxFTSFetchLimit = %d, want 5000", maxFTSFetchLimit)
	}
}

// TestSearchCapsResponseAtMaxTopK drives the whole Search path with an
// oversized top_k, the way an MCP caller can.
func TestSearchCapsResponseAtMaxTopK(t *testing.T) {
	dims := 8
	embeddingServer := newEmbeddingServer(t, dims)
	defer embeddingServer.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embeddingServer.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Search.SimilarityThreshold = 0.0
	cfg.Search.MinScore = 0
	cfg.Search.DefaultTopK = 20

	dbPath := filepath.Join(t.TempDir(), "topk-cap.db")
	db, err := store.Open(dbPath, dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if !db.VecAvailable() {
		t.Skip("vec0 not available")
	}

	// More rows than the ceiling, so a clamped and an unclamped run differ.
	const corpus = maxTopK + 500
	entries := make([]store.Entry, corpus)
	for i := range entries {
		vec := make([]float32, dims)
		vec[0] = 1
		entries[i] = store.Entry{
			LineNum:   i + 1,
			EndLine:   i + 1,
			Text:      fmt.Sprintf("uniquetoken chunk number %d", i),
			Embedding: vec,
			Folder:    "src",
			Language:  "go",
		}
	}
	if err := db.InsertEntries("src", "corpus.go", entries); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}

	embedClient := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, dims, false, false)
	searcher := NewSearcher(db, embedClient, &cfg, nil)

	results, err := searcher.Search(context.Background(), "uniquetoken", 5000, nil)
	if err != nil {
		t.Fatalf("search with top_k=5000: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results at top_k=5000")
	}
	if len(results) > maxTopK {
		t.Fatalf("got %d results, want no more than the %d ceiling", len(results), maxTopK)
	}
}
