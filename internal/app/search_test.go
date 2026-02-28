package app

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/search"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func testEmbeddingVector(text string, dims int) []float32 {
	vec := make([]float32, dims)
	for _, tok := range strings.Fields(text) {
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

func newAppTestEmbeddingServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()

	type embReq struct {
		Input []string `json:"input"`
	}
	type embDatum struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	}
	type embResp struct {
		Data []embDatum `json:"data"`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req embReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := embResp{Data: make([]embDatum, 0, len(req.Input))}
		for i, text := range req.Input {
			resp.Data = append(resp.Data, embDatum{
				Embedding: testEmbeddingVector(text, dims),
				Index:     i,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestSearchService(t *testing.T) {
	ctx := context.Background()
	dims := 256

	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "main.go"), []byte(`package main

func main() {
	fmt.Println("hello world")
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	embSrv := newAppTestEmbeddingServer(t, dims)
	defer embSrv.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embSrv.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Search.SimilarityThreshold = 0.0
	cfg.Search.MinScore = 0
	cfg.Search.DefaultTopK = 5
	cfg.Folders = []config.FolderConfig{
		{Path: corpus, Description: "test"},
	}

	dbPath := filepath.Join(t.TempDir(), "search-svc.db")
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
		t.Fatalf("index: %v", err)
	}

	searcher := search.NewSearcher(db, embedClient, &cfg, nil)
	svc := NewSearchService(searcher)

	tests := []struct {
		name    string
		query   string
		topK    int
		folders []string
		wantErr bool
		check   func(t *testing.T, results []domain.SearchResult)
	}{
		{
			name:  "returns domain results",
			query: "main",
			topK:  5,
			check: func(t *testing.T, results []domain.SearchResult) {
				t.Helper()
				if len(results) == 0 {
					t.Fatal("expected results")
				}
				for _, r := range results {
					if r.FilePath == "" {
						t.Error("FilePath should not be empty")
					}
				}
			},
		},
		{
			name:  "empty query returns empty",
			query: "",
			topK:  5,
			check: func(t *testing.T, results []domain.SearchResult) {
				t.Helper()
				if len(results) != 0 {
					t.Errorf("expected 0 results, got %d", len(results))
				}
			},
		},
		{
			name:    "respects folder filter",
			query:   "main",
			topK:    5,
			folders: []string{corpus},
			check: func(t *testing.T, results []domain.SearchResult) {
				t.Helper()
				if len(results) == 0 {
					t.Fatal("expected results")
				}
				for _, r := range results {
					if r.Folder != corpus {
						t.Errorf("expected folder %s, got %s", corpus, r.Folder)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := svc.Search(ctx, tt.query, tt.topK, tt.folders)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, results)
			}
		})
	}
}
