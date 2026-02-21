//go:build eval

package search

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/index"
	"github.com/akhmetov/poisk/internal/store"
)

type evalQuery struct {
	Query         string   `json:"query"`
	RelevantFiles []string `json:"relevant_files"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve eval test path: runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
}

func loadEvalQueries(t *testing.T, root string) []evalQuery {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "eval", "queries.json"))
	if err != nil {
		t.Fatalf("load eval queries: %v", err)
	}
	var queries []evalQuery
	if err := json.Unmarshal(data, &queries); err != nil {
		t.Fatalf("parse eval queries: %v", err)
	}
	return queries
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func buildEvalCorpus(t *testing.T, root string, queries []evalQuery) string {
	t.Helper()

	files := make(map[string]bool)
	for _, q := range queries {
		for _, rel := range q.RelevantFiles {
			files[canonicalPath(rel)] = true
		}
	}

	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files[canonicalPath(rel)] = true
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}

	corpusRoot := filepath.Join(t.TempDir(), "corpus")
	for rel := range files {
		src := filepath.Join(root, filepath.FromSlash(rel))
		dst := filepath.Join(corpusRoot, filepath.FromSlash(rel))
		copyFile(t, src, dst)
	}

	return corpusRoot
}

func hashToken(token string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return h.Sum32()
}

func testEmbedding(text string, dims int) []float32 {
	vec := make([]float32, dims)
	for _, tok := range tokenize(text) {
		idx := int(hashToken(tok) % uint32(dims))
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

func newTestEmbeddingServer(t *testing.T, dims int) *httptest.Server {
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				Embedding: testEmbedding(text, dims),
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(handler)
}

func canonicalPath(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}

func pathMatches(resultPath, relevantPath string) bool {
	rp := canonicalPath(resultPath)
	rel := canonicalPath(relevantPath)
	return rp == rel || strings.HasSuffix(rp, "/"+rel)
}

func TestEvalRecallAndMRR(t *testing.T) {
	root := repoRoot(t)
	queries := loadEvalQueries(t, root)
	k := 10
	corpusRoot := buildEvalCorpus(t, root, queries)

	cfg := config.DefaultConfig()
	cfg.Embedding.Model = "eval-test-embedding"
	cfg.Embedding.Dimensions = 64
	cfg.Embedding.BatchSize = 128
	cfg.Index.Languages = []string{"go", "python", "rust", "javascript", "typescript"}
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Search.DefaultTopK = k
	cfg.Search.SimilarityThreshold = 0.0
	cfg.Folders = []config.FolderConfig{
		{Path: corpusRoot, Description: "eval-corpus"},
	}

	embeddingServer := newTestEmbeddingServer(t, cfg.Embedding.Dimensions)
	defer embeddingServer.Close()
	cfg.Embedding.BaseURL = embeddingServer.URL

	dbPath := filepath.Join(t.TempDir(), "eval.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if !db.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	client := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	indexer := index.NewIndexer(db, client, &cfg)
	searcher := NewSearcher(db, client, &cfg, nil)

	stats, err := indexer.IndexAll(context.Background())
	if err != nil {
		t.Fatalf("index eval corpus: %v", err)
	}
	if len(stats) == 0 || stats[0].ChunksCreated == 0 {
		t.Fatalf("index eval corpus: no chunks created (stats=%+v)", stats)
	}

	var totalRecall float64
	var totalMRR float64

	for _, q := range queries {
		results, searchErr := searcher.Search(context.Background(), q.Query, k, nil)
		if searchErr != nil {
			t.Fatalf("query %q failed: %v", q.Query, searchErr)
		}

		hits := make(map[string]bool)
		firstHitRank := 0

		for rank, r := range results {
			for _, rel := range q.RelevantFiles {
				if pathMatches(r.FilePath, rel) {
					hits[rel] = true
					if firstHitRank == 0 {
						firstHitRank = rank + 1
					}
				}
			}
		}

		recall := float64(len(hits)) / float64(len(q.RelevantFiles))
		if len(hits) == 0 {
			paths := make([]string, 0, min(len(results), 3))
			for _, r := range results[:min(len(results), 3)] {
				paths = append(paths, canonicalPath(r.FilePath))
			}
			t.Logf("miss: query=%q expected=%v top=%v", q.Query, q.RelevantFiles, paths)
		}

		totalRecall += recall

		mrr := 0.0
		if firstHitRank > 0 {
			mrr = 1.0 / float64(firstHitRank)
		}
		totalMRR += mrr
	}

	n := float64(len(queries))
	avgRecall := totalRecall / n
	avgMRR := totalMRR / n

	fmt.Printf("\n=== Evaluation Results ===\n")
	fmt.Printf("Queries:     %d\n", len(queries))
	fmt.Printf("Recall@%d:   %.3f\n", k, avgRecall)
	fmt.Printf("MRR:         %.3f\n", avgMRR)
	fmt.Printf("==========================\n\n")

	// Baselines for end-to-end retrieval + ranking over the local eval corpus.
	if avgRecall < 0.75 {
		t.Errorf("Recall@%d = %.3f, want >= 0.75", k, avgRecall)
	}
	if avgMRR < 0.60 {
		t.Errorf("MRR = %.3f, want >= 0.60", avgMRR)
	}
}
