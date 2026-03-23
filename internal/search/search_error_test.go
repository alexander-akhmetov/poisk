package search

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func testSearchConfig(dims int) config.Config {
	cfg := config.DefaultConfig()
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.DefaultTopK = 5
	cfg.Search.SimilarityThreshold = 0.0
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	return cfg
}

func TestSearchReturnsPartialResultsWhenVecFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-partial-failure.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertChunks("repo", "target.go", []domain.ChunkWithEmbedding{
		{
			Chunk: domain.Chunk{
				LineNum:  10,
				EndLine:  20,
				Text:     "binary search algorithm implementation details",
				Folder:   "repo",
				Language: "go",
			},
			Embedding: []float32{1, 0, 0},
		},
	}); err != nil {
		t.Fatalf("insert fixture entry: %v", err)
	}

	cfg := testSearchConfig(3)
	cfg.Embedding.BaseURL = "http://127.0.0.1:1"

	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	searcher := NewSearcher(s, client, &cfg, nil)

	results, searchErr := searcher.Search(context.Background(), "binary search algorithm", 5, nil)
	if searchErr == nil {
		t.Fatalf("expected partial vec error, got nil")
	}
	if !strings.Contains(searchErr.Error(), "embedding request") {
		t.Fatalf("expected embedding request error, got %v", searchErr)
	}
	if len(results) == 0 {
		t.Fatalf("expected FTS results despite vec failure")
	}
}

func TestSearchReturnsVecFailureWhenFTSModeIsDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-vec-only-failure.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	cfg := testSearchConfig(3)
	cfg.Embedding.BaseURL = "http://127.0.0.1:1"

	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	searcher := NewSearcher(s, client, &cfg, nil)

	results, searchErr := searcher.Search(context.Background(), "vec:binary search", 5, nil)
	if searchErr == nil {
		t.Fatalf("expected vec-only error, got nil")
	}
	if !strings.Contains(searchErr.Error(), "vec failed, no FTS results") {
		t.Fatalf("expected vec-only failure message, got %v", searchErr)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results on vec-only failure, got %d", len(results))
	}
}

func TestSearchReturnsFTSFailureWhenVecModeIsDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-fts-only-failure.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if !s.FTSAvailable() {
		_ = s.Close()
		t.Skip("FTS5 not available")
	}

	cfg := testSearchConfig(3)
	cfg.Embedding.BaseURL = "http://127.0.0.1:1"

	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	searcher := NewSearcher(s, client, &cfg, nil)

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	results, searchErr := searcher.Search(context.Background(), "lex:binary search", 5, nil)
	if searchErr == nil {
		t.Fatalf("expected FTS-only error, got nil")
	}
	if !strings.Contains(searchErr.Error(), "FTS failed, no vec results") {
		t.Fatalf("expected FTS-only failure message, got %v", searchErr)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results on FTS-only failure, got %d", len(results))
	}
}

func TestSearchReturnsCombinedBackendFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-all-failure.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if !s.FTSAvailable() {
		_ = s.Close()
		t.Skip("FTS5 not available")
	}

	cfg := testSearchConfig(3)
	cfg.Embedding.BaseURL = "http://127.0.0.1:1"

	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	searcher := NewSearcher(s, client, &cfg, nil)

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	results, searchErr := searcher.Search(context.Background(), "hybrid backend failure", 5, nil)
	if searchErr == nil {
		t.Fatalf("expected combined backend error, got nil")
	}
	if !strings.Contains(searchErr.Error(), "all search backends failed") {
		t.Fatalf("expected combined failure message, got %v", searchErr)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results on combined backend failure, got %d", len(results))
	}
}
