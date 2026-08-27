package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/config"
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
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertEntries("repo", "target.go", []store.Entry{
		{
			LineNum:   10,
			EndLine:   20,
			Text:      "binary search algorithm implementation details",
			Embedding: []float32{1, 0, 0},
			Folder:    "repo",
			Language:  "go",
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
		cfg.Embedding.Matryoshka,
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

func TestSearchEmbeddingTimeoutFallsBackToFTS(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-embedding-timeout.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertEntries("repo", "target.go", []store.Entry{{
		LineNum:   10,
		EndLine:   20,
		Text:      "binary search algorithm implementation details",
		Embedding: []float32{1, 0, 0},
		Folder:    "repo",
		Language:  "go",
	}}); err != nil {
		t.Fatalf("insert fixture entry: %v", err)
	}

	requestStarted := make(chan struct{}, 1)
	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestStarted <- struct{}{}
		time.Sleep(500 * time.Millisecond)
		http.Error(w, "request exceeded the search deadline", http.StatusInternalServerError)
	}))
	t.Cleanup(embedServer.Close)

	cfg := testSearchConfig(3)
	cfg.Embedding.BaseURL = embedServer.URL
	cfg.Search.EmbeddingTimeout = 100 * time.Millisecond
	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
		cfg.Embedding.Matryoshka,
	)
	searcher := NewSearcher(s, client, &cfg, nil)

	results, searchErr := searcher.Search(context.Background(), "binary search algorithm", 5, nil)

	if searchErr == nil || !strings.Contains(searchErr.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("search error = %v, want embedding deadline", searchErr)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS results after embedding timeout")
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("embedding request did not reach the server")
	}
}

func TestSearchReturnsVecFailureWhenFTSModeIsDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-vec-only-failure.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
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
		cfg.Embedding.Matryoshka,
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
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
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
		cfg.Embedding.Matryoshka,
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
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
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
		cfg.Embedding.Matryoshka,
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
