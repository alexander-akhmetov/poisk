package search

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/llm"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// Scale benchmarks run against a synthetic index big enough for the vec0 scan
// and the FTS index to behave like a real corpus. Building it costs a minute,
// so the database is cached at POISK_BENCH_DB (default: a fixed path under the
// OS temp dir) and reused until POISK_BENCH_N changes.
//
//	go test -tags fts5 -run '^$' -bench BenchmarkScale ./internal/search/
const (
	benchDims    = 1024
	benchSources = 4
	benchVocab   = 4000
)

func benchN() int {
	if v := os.Getenv("POISK_BENCH_N"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 200_000
}

func benchDBPath(n int) string {
	if v := os.Getenv("POISK_BENCH_DB"); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("poisk-bench-%d-%d.db", n, benchDims))
}

func benchSourceName(i int) string {
	return fmt.Sprintf("/bench/source%d", i%benchSources)
}

// benchVector returns a deterministic unit vector.
func benchVector(rng *rand.Rand) []float32 {
	v := make([]float32, benchDims)
	var sum float64
	for i := range v {
		x := float32(rng.NormFloat64())
		v[i] = x
		sum += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

// benchDensities maps a marker term to the fraction of chunks that contain it.
// Query cost in FTS5 scales with how many rows a term matches, so the corpus
// carries terms at known densities and the benchmarks name the density they
// exercise instead of guessing from a random distribution.
// Ordered, because map iteration order would vary the RNG draw sequence and
// make the generated corpus irreproducible.
var benchDensities = []struct {
	term    string
	density float64
}{
	{"densehalf", 0.50},
	{"densetenth", 0.10},
	{"densepercent", 0.01},
	{"denserare", 0.001},
}

// benchText builds a chunk of pseudo-code text: the density markers above plus
// a Zipf-like tail of vocabulary terms, so a query can hit either a handful of
// rows or a large slice of the corpus.
func benchText(rng *rand.Rand, i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func handleRequest%d(ctx context.Context) error {\n", i)
	for _, d := range benchDensities {
		if rng.Float64() < d.density {
			fmt.Fprintf(&b, "\t// %s marker\n", d.term)
		}
	}
	for range 12 {
		term := int(math.Abs(rng.NormFloat64()) * float64(benchVocab) / 3)
		if term >= benchVocab {
			term = benchVocab - 1
		}
		fmt.Fprintf(&b, "\t// term%04d parseValue%d handler_config value%d\n", term, rng.Intn(50), rng.Intn(1000))
	}
	b.WriteString("\treturn nil\n}\n")
	return b.String()
}

// buildBenchDB populates a store directly with SQL. It bypasses the indexer so
// no embedding server is needed.
func buildBenchDB(tb testing.TB, path string, n int) {
	tb.Helper()

	s, err := store.Open(path, benchDims, store.QuantizationInt8)
	if err != nil {
		tb.Fatalf("open bench store: %v", err)
	}
	defer s.Close()
	if !s.VecAvailable() || !s.FTSAvailable() {
		tb.Fatalf("bench db needs vec0 and fts5 (vec=%v fts=%v)", s.VecAvailable(), s.FTSAvailable())
	}

	db := s.DB()
	tx, err := db.Begin()
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	insEmb, err := tx.Prepare(`INSERT INTO embeddings
		(id, source, file_path, line_num, chunk_text, folder, end_line, language, chunk_kind, symbol)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer insEmb.Close()
	insVec, err := tx.Prepare(`INSERT INTO vec_embeddings (rowid, source, embedding) VALUES (?, ?, ` + s.VecValueExpr() + `)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer insVec.Close()
	insFTS, err := tx.Prepare(`INSERT INTO chunks_fts
		(rowid, chunk_text, source, file_path, line_num, folder, end_line, language, chunk_kind, symbol)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer insFTS.Close()

	rng := rand.New(rand.NewSource(42))
	for i := 1; i <= n; i++ {
		src := benchSourceName(i)
		file := fmt.Sprintf("%s/pkg%d/file%d.go", src, i%500, i)
		text := benchText(rng, i)
		symbol := fmt.Sprintf("handleRequest%d", i)
		line := (i % 400) * 3

		if _, err := insEmb.Exec(i, src, file, line, text, src, line+20, "go", "function_declaration", symbol); err != nil {
			tb.Fatalf("insert embeddings row %d: %v", i, err)
		}
		if _, err := insVec.Exec(i, src, store.Float32sToBlob(benchVector(rng))); err != nil {
			tb.Fatalf("insert vec row %d: %v", i, err)
		}
		if _, err := insFTS.Exec(i, text, src, file, line, src, line+20, "go", "function_declaration", symbol); err != nil {
			tb.Fatalf("insert fts row %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatalf("commit bench db: %v", err)
	}
	if _, err := db.Exec("ANALYZE"); err != nil {
		tb.Fatalf("analyze: %v", err)
	}
}

// openBenchDB returns a store over the cached synthetic index, building it on
// first use.
func openBenchDB(tb testing.TB) (*store.Store, int) {
	tb.Helper()
	n := benchN()
	path := benchDBPath(n)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		tb.Logf("building bench db with %d rows at %s", n, path)
		buildBenchDB(tb, path, n)
	}

	s, err := store.Open(path, benchDims, store.QuantizationInt8)
	if err != nil {
		tb.Fatalf("open bench store: %v", err)
	}
	tb.Cleanup(func() { s.Close() })

	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&count); err != nil {
		tb.Fatal(err)
	}
	if count != n {
		tb.Fatalf("bench db has %d rows, want %d (delete %s to rebuild)", count, n, path)
	}
	return s, n
}

func benchQueryVector() []byte {
	return store.Float32sToBlob(benchVector(rand.New(rand.NewSource(7))))
}

func BenchmarkScaleVec(b *testing.B) {
	s, _ := openBenchDB(b)
	blob := benchQueryVector()

	for _, topK := range []int{20, 100} {
		b.Run(fmt.Sprintf("topK=%d", topK), func(b *testing.B) {
			for b.Loop() {
				res, err := searchVec(s, blob, topK, nil, MetadataFilters{}, 0.0)
				if err != nil {
					b.Fatal(err)
				}
				if len(res) == 0 {
					b.Fatal("no vec results")
				}
			}
		})
	}
}

func BenchmarkScaleVecScoped(b *testing.B) {
	s, _ := openBenchDB(b)
	blob := benchQueryVector()
	folders := []string{benchSourceName(0)}

	for b.Loop() {
		if _, err := searchVec(s, blob, 20, folders, MetadataFilters{}, 0.0); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScaleVecFanout compares running several vector searches back to
// back against running them concurrently. Query expansion turns one search
// into one scan per variant, so this measures what parallelising that fan-out
// can win. Each vec0 scan is single-threaded and memory-bound, so the speedup
// is not assumed.
func BenchmarkScaleVecFanout(b *testing.B) {
	s, _ := openBenchDB(b)
	const variants = 4
	blobs := make([][]byte, variants)
	for i := range blobs {
		blobs[i] = store.Float32sToBlob(benchVector(rand.New(rand.NewSource(int64(i + 1)))))
	}

	b.Run("sequential", func(b *testing.B) {
		for b.Loop() {
			for _, blob := range blobs {
				if _, err := searchVec(s, blob, 20, nil, MetadataFilters{}, 0.0); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("parallel", func(b *testing.B) {
		for b.Loop() {
			var wg sync.WaitGroup
			errs := make([]error, variants)
			for i, blob := range blobs {
				wg.Go(func() {
					_, errs[i] = searchVec(s, blob, 20, nil, MetadataFilters{}, 0.0)
				})
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func BenchmarkScaleFTS(b *testing.B) {
	s, _ := openBenchDB(b)

	for _, tc := range benchFTSCases {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := searchFTS(s, tc.query, 20, nil, MetadataFilters{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

var benchFTSCases = []struct {
	name  string
	query string
}{
	{"density=0.1pct", "denserare marker"},
	{"density=1pct", "densepercent marker"},
	{"density=10pct", "densetenth marker"},
	{"density=50pct", "densehalf marker"},
	{"nomatch", "zzzznotpresent qqqqmissing"},
}

// BenchmarkScaleFTSStage measures one FTS query, without the staged retry
// logic, to separate per-stage cost from the number of stages searchFTS runs.
func BenchmarkScaleFTSStage(b *testing.B) {
	s, _ := openBenchDB(b)
	for _, tc := range benchFTSCases {
		b.Run(tc.name, func(b *testing.B) {
			q := buildStrictAND(tokenize(tc.query))
			for b.Loop() {
				if _, err := queryFTS(s, q, ftsFetchLimit(20), nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkScaleFTSNoText runs the same stage query without chunk_text in the
// projection. chunks_fts is external-content, so every returned chunk_text is
// a lookup back into embeddings; the delta against BenchmarkScaleFTSStage is
// what deferring the text fetch until after ranking could save.
func BenchmarkScaleFTSNoText(b *testing.B) {
	s, _ := openBenchDB(b)
	for _, tc := range benchFTSCases {
		b.Run(tc.name, func(b *testing.B) {
			q := buildStrictAND(tokenize(tc.query))
			limit := ftsFetchLimit(20)
			for b.Loop() {
				rows, err := s.DB().Query(
					`SELECT rowid, bm25(chunks_fts) AS rank FROM chunks_fts
					 WHERE chunks_fts MATCH ? ORDER BY rank ASC LIMIT ?`, q, limit)
				if err != nil {
					b.Fatal(err)
				}
				for rows.Next() {
					var id int64
					var rank float64
					if err := rows.Scan(&id, &rank); err != nil {
						b.Fatal(err)
					}
				}
				rows.Close()
			}
		})
	}
}

// Latencies measured against a self-hosted llama.cpp server on a LAN, used so
// the end-to-end benchmark reflects where a real search spends its time
// instead of assuming instant network calls.
const (
	benchEmbedBaseLatency = 200 * time.Millisecond
	benchEmbedPerInput    = 40 * time.Millisecond
	benchExpandLatency    = 520 * time.Millisecond
	benchRerankLatency    = 980 * time.Millisecond
)

func benchLatentEmbedServer(tb testing.TB) *httptest.Server {
	tb.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		time.Sleep(benchEmbedBaseLatency + time.Duration(len(req.Input))*benchEmbedPerInput)

		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		resp := struct {
			Data []datum `json:"data"`
		}{}
		rng := rand.New(rand.NewSource(11))
		for i := range req.Input {
			resp.Data = append(resp.Data, datum{Embedding: benchVector(rng), Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	tb.Cleanup(srv.Close)
	return srv
}

// benchLatentLLMServer answers expansion with three variants and reranking
// with a score array, each after the latency that stage really costs.
func benchLatentLLMServer(tb testing.TB) *httptest.Server {
	tb.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[0].Content
		}

		content := "densetenth marker\ndensepercent marker\nhandleRequest handler\n"
		if strings.Contains(prompt, "Rate the relevance") {
			time.Sleep(benchRerankLatency)
			scores := make([]string, 20)
			for i := range scores {
				scores[i] = "7"
			}
			content = "{\"scores\":[" + strings.Join(scores, ",") + "]}"
		} else {
			time.Sleep(benchExpandLatency)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}}},
		})
	}))
	tb.Cleanup(srv.Close)
	return srv
}

// BenchmarkScaleEndToEnd measures a full Search against the 200k-row index
// with query expansion and reranking on, the configuration that pays for every
// stage.
func BenchmarkScaleEndToEnd(b *testing.B) {
	s, _ := openBenchDB(b)
	embedSrv := benchLatentEmbedServer(b)
	llmSrv := benchLatentLLMServer(b)

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Dimensions = benchDims
	cfg.Search.SimilarityThreshold = 0
	cfg.Search.MinScore = 0

	embedClient := embed.NewClient(embedSrv.URL, "", "bench", benchDims, false, false)
	llmClient := llm.NewClient(llmSrv.URL, "", "bench")
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		expansion bool
		rerank    bool
	}{
		{"plain", false, false},
		{"expansion", true, false},
		{"expansion+rerank", true, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := cfg
			cfg.Search.QueryExpansion = tc.expansion
			cfg.Search.Rerank = tc.rerank
			searcher := NewSearcher(s, embedClient, &cfg, llmClient)
			for b.Loop() {
				res, err := searcher.Search(ctx, "densetenth marker handler", 20, nil)
				if err != nil {
					b.Fatal(err)
				}
				if len(res) == 0 {
					b.Fatal("no results")
				}
			}
		})
	}
}

func BenchmarkScaleHybrid(b *testing.B) {
	s, _ := openBenchDB(b)
	blob := benchQueryVector()
	ctx := context.Background()
	_ = ctx

	for b.Loop() {
		vec, err := searchVec(s, blob, 20, nil, MetadataFilters{}, 0.0)
		if err != nil {
			b.Fatal(err)
		}
		fts, err := searchFTS(s, "retryBackoff handler_config", 20, nil, MetadataFilters{})
		if err != nil {
			b.Fatal(err)
		}
		mergeResults(vec, fts, 60, 20)
	}
}
