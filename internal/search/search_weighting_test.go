package search

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/llm"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func testVector(text string, dims int) []float32 {
	vec := make([]float32, dims)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		idx := int(h.Sum32() % uint32(dims))
		vec[idx] += 1.0
	}

	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm == 0 {
		vec[0] = 1.0
		return vec
	}
	scale := float32(1.0 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}
	return vec
}

func newEmbeddingServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()

	type embeddingRequest struct {
		Input []string `json:"input"`
	}
	type embeddingDatum struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	}
	type embeddingResponse struct {
		Data []embeddingDatum `json:"data"`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := embeddingResponse{Data: make([]embeddingDatum, 0, len(req.Input))}
		for i, text := range req.Input {
			resp.Data = append(resp.Data, embeddingDatum{
				Embedding: testVector(text, dims),
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newExpansionServer(t *testing.T, expanded string) *httptest.Server {
	t.Helper()

	type choice struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	type completionResponse struct {
		Choices []choice `json:"choices"`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		resp := completionResponse{
			Choices: []choice{{Message: struct {
				Content string `json:"content"`
			}{Content: expanded}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestSearchExpansionWeightingAffectsRank(t *testing.T) {
	ctx := context.Background()
	dims := 256
	corpus := t.TempDir()

	originalPath := filepath.Join(corpus, "original.md")
	expandedPath := filepath.Join(corpus, "expanded.md")
	if err := os.WriteFile(originalPath, []byte("tokenalpha tokenalpha tokenalpha tokenalpha tokenalpha"), 0o644); err != nil {
		t.Fatalf("write original fixture: %v", err)
	}
	if err := os.WriteFile(expandedPath, []byte("tokenbeta tokenbeta tokenbeta tokenbeta tokenbeta"), 0o644); err != nil {
		t.Fatalf("write expanded fixture: %v", err)
	}

	embeddingServer := newEmbeddingServer(t, dims)
	defer embeddingServer.Close()
	expansionServer := newExpansionServer(t, "tokenbeta")
	defer expansionServer.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embeddingServer.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = true
	cfg.Search.SimilarityThreshold = 0.8
	cfg.Search.DefaultTopK = 5
	cfg.Folders = []config.FolderConfig{
		{Path: corpus, Description: "test-corpus"},
	}

	dbPath := filepath.Join(t.TempDir(), "search-weighting.db")
	db, err := store.Open(dbPath, dims)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if !db.VecAvailable() {
		t.Skip("vec0 not available")
	}

	embedClient := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	indexer := index.NewIndexer(db, embedClient, &cfg)
	if _, err := indexer.IndexAll(ctx); err != nil {
		t.Fatalf("index fixtures: %v", err)
	}

	llmClient := llm.NewClient(expansionServer.URL, "", "test")
	searcher := NewSearcher(db, embedClient, &cfg, llmClient)

	results, err := searcher.Search(ctx, "vec:tokenalpha", 5, nil)
	if err != nil {
		t.Fatalf("search with default weights: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results with expansion, got %d", len(results))
	}
	if filepath.Base(results[0].FilePath) != "original.md" {
		t.Fatalf("default weights should favor original query result first, got %s", results[0].FilePath)
	}

	cfg.Search.ExpandedQueryWeight = 2.0
	boosted := NewSearcher(db, embedClient, &cfg, llmClient)
	boostedResults, err := boosted.Search(ctx, "vec:tokenalpha", 5, nil)
	if err != nil {
		t.Fatalf("search with boosted expanded weight: %v", err)
	}
	if len(boostedResults) < 2 {
		t.Fatalf("expected at least 2 boosted results, got %d", len(boostedResults))
	}
	if filepath.Base(boostedResults[0].FilePath) != "expanded.md" {
		t.Fatalf("boosted expanded weight should move expanded result first, got %s", boostedResults[0].FilePath)
	}
}
