package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// benchCorpus creates a temp directory with N Go files, each containing a
// function with unique name.
func benchCorpus(b *testing.B, n int) string {
	b.Helper()
	dir, _ := filepath.EvalSymlinks(b.TempDir())
	for i := range n {
		content := fmt.Sprintf(`package bench

// Function%d performs operation number %d on the input data.
func Function%d(data []int) int {
    sum := 0
    for _, v := range data {
        sum += v * %d
    }
    return sum
}
`, i, i, i, i+1)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", i)), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

func benchEmbedServer(b *testing.B, dims int) *httptest.Server {
	b.Helper()
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
		for i, text := range req.Input {
			resp.Data = append(resp.Data, datum{
				Embedding: testVector(text, dims),
				Index:     i,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

type benchStack struct {
	db       *store.Store
	indexer  *index.Indexer
	searcher *Searcher
	cfg      config.Config
	corpus   string
}

func newBenchStack(b *testing.B, corpusSize int) *benchStack {
	b.Helper()
	dims := 64
	corpus := benchCorpus(b, corpusSize)

	embedSrv := benchEmbedServer(b, dims)
	b.Cleanup(embedSrv.Close)

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "bench-model"
	cfg.Embedding.Dimensions = dims
	cfg.Embedding.BatchSize = 50
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Folders = []config.FolderConfig{{Path: corpus, Description: "bench"}}

	dbPath := filepath.Join(b.TempDir(), "bench.db")
	db, err := store.Open(dbPath, dims, cfg.Embedding.Quantization)
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	b.Cleanup(func() { db.Close() })

	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, dims, false, false)
	indexer := index.NewIndexer(db, client, &cfg)
	searcher := NewSearcher(db, client, &cfg, nil)

	return &benchStack{
		db:       db,
		indexer:  indexer,
		searcher: searcher,
		cfg:      cfg,
		corpus:   corpus,
	}
}

func BenchmarkIndexSmall(b *testing.B) {
	bs := newBenchStack(b, 10)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := bs.db.ClearSource(bs.corpus); err != nil {
			b.Fatal(err)
		}
		if _, err := bs.indexer.IndexAll(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndexMedium(b *testing.B) {
	bs := newBenchStack(b, 50)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := bs.db.ClearSource(bs.corpus); err != nil {
			b.Fatal(err)
		}
		if _, err := bs.indexer.IndexAll(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchFTSOnly(b *testing.B) {
	bs := newBenchStack(b, 50)
	ctx := context.Background()
	if _, err := bs.indexer.IndexAll(ctx); err != nil {
		b.Fatalf("index: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := bs.searcher.Search(ctx, "lex:Function", 20, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchVecOnly(b *testing.B) {
	bs := newBenchStack(b, 50)
	ctx := context.Background()
	if _, err := bs.indexer.IndexAll(ctx); err != nil {
		b.Fatalf("index: %v", err)
	}
	if !bs.db.VecAvailable() {
		b.Skip("vec0 not available")
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := bs.searcher.Search(ctx, "vec:function operation data", 20, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchHybrid(b *testing.B) {
	bs := newBenchStack(b, 50)
	ctx := context.Background()
	if _, err := bs.indexer.IndexAll(ctx); err != nil {
		b.Fatalf("index: %v", err)
	}
	if !bs.db.VecAvailable() {
		b.Skip("vec0 not available")
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := bs.searcher.Search(ctx, "function operation data", 20, nil); err != nil {
			b.Fatal(err)
		}
	}
}
