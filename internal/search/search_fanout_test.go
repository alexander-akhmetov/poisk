package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/llm"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// countingEmbeddingServer records every request body it serves.
type countingEmbeddingServer struct {
	*httptest.Server
	mu     sync.Mutex
	inputs [][]string
}

func (c *countingEmbeddingServer) requests() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]string, len(c.inputs))
	copy(out, c.inputs)
	return out
}

func newCountingEmbeddingServer(t *testing.T, dims int) *countingEmbeddingServer {
	t.Helper()
	cs := &countingEmbeddingServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cs.mu.Lock()
		cs.inputs = append(cs.inputs, req.Input)
		cs.mu.Unlock()

		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		resp := struct {
			Data []datum `json:"data"`
		}{}
		for i, text := range req.Input {
			resp.Data = append(resp.Data, datum{Embedding: testVector(text, dims), Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(cs.Close)
	return cs
}

// newMultiExpansionServer returns an expansion server that answers with
// several variants, one per line.
func newMultiExpansionServer(t *testing.T, variants []string) *httptest.Server {
	t.Helper()
	var sb strings.Builder
	for _, v := range variants {
		sb.WriteString(v + "\n")
	}
	body := sb.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": body}}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSearchEmbedsExpandedQueriesInOneBatch pins the round-trip count: query
// expansion produces several query variants, and embedding them one at a time
// costs one network round trip each.
func TestSearchEmbedsExpandedQueriesInOneBatch(t *testing.T) {
	ctx := context.Background()
	dims := 256
	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "doc.md"),
		[]byte("tokenalpha tokenbeta tokengamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	embedSrv := newCountingEmbeddingServer(t, dims)
	expandSrv := newMultiExpansionServer(t, []string{"tokenbeta", "tokengamma"})

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = true
	cfg.Search.Rerank = false
	cfg.Search.MinScore = 0
	cfg.Folders = []config.FolderConfig{{Path: corpus, Description: "test-corpus"}}

	db, err := store.Open(filepath.Join(t.TempDir(), "fanout.db"), dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !db.VecAvailable() {
		t.Skip("vec0 not available")
	}

	embedClient := embed.NewClient(embedSrv.URL, "", cfg.Embedding.Model, dims, false, false)
	if _, err := index.NewIndexer(db, embedClient, &cfg).IndexAll(ctx); err != nil {
		t.Fatalf("index fixtures: %v", err)
	}

	indexingRequests := len(embedSrv.requests())
	searcher := NewSearcher(db, embedClient, &cfg, llm.NewClient(expandSrv.URL, "", "test"))
	if _, err := searcher.Search(ctx, "tokenalpha", 5, nil); err != nil {
		t.Fatalf("search: %v", err)
	}

	searchRequests := embedSrv.requests()[indexingRequests:]
	if len(searchRequests) != 1 {
		t.Fatalf("search made %d embedding requests, want 1 batched request: %v",
			len(searchRequests), searchRequests)
	}
	if got := len(searchRequests[0]); got != 3 {
		t.Errorf("batch carried %d queries, want 3 (original + 2 variants): %v",
			got, searchRequests[0])
	}
}

// TestSearchFanoutMatchesSequential checks that running the retrieval tasks
// concurrently returns the same ranking as running them one at a time.
func TestSearchFanoutMatchesSequential(t *testing.T) {
	ctx := context.Background()
	dims := 256
	corpus := t.TempDir()
	for i, text := range []string{
		"tokenalpha retry backoff handler",
		"tokenbeta retry configuration",
		"tokengamma handler configuration",
		"tokendelta unrelated content",
	} {
		if err := os.WriteFile(filepath.Join(corpus, string(rune('a'+i))+".md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	embedSrv := newCountingEmbeddingServer(t, dims)
	expandSrv := newMultiExpansionServer(t, []string{"tokenbeta", "tokengamma"})

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = true
	cfg.Search.Rerank = false
	cfg.Search.MinScore = 0
	cfg.Search.SimilarityThreshold = 0
	cfg.Folders = []config.FolderConfig{{Path: corpus, Description: "test-corpus"}}

	db, err := store.Open(filepath.Join(t.TempDir(), "fanout-order.db"), dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !db.VecAvailable() {
		t.Skip("vec0 not available")
	}

	embedClient := embed.NewClient(embedSrv.URL, "", cfg.Embedding.Model, dims, false, false)
	if _, err := index.NewIndexer(db, embedClient, &cfg).IndexAll(ctx); err != nil {
		t.Fatalf("index fixtures: %v", err)
	}
	searcher := NewSearcher(db, embedClient, &cfg, llm.NewClient(expandSrv.URL, "", "test"))

	first, err := searcher.Search(ctx, "tokenalpha retry", 10, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("no results")
	}

	// Concurrency must not make the ranking depend on completion order.
	for range 5 {
		again, err := searcher.Search(ctx, "tokenalpha retry", 10, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("result count changed between runs: %d then %d", len(first), len(again))
		}
		for i := range first {
			if first[i].FilePath != again[i].FilePath || first[i].Score != again[i].Score {
				t.Fatalf("rank %d changed between runs: %s (%v) then %s (%v)",
					i, first[i].FilePath, first[i].Score, again[i].FilePath, again[i].Score)
			}
		}
	}
}
