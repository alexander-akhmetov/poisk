package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/llm"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

type Result struct {
	FilePath string
	LineNum  int
	EndLine  int
	Text     string
	Score    float64
	Folder   string
	Language string
	Kind     string
	Symbol   string
	Context  []string
}

// maxTopK bounds how many results a caller can ask for. top_k arrives
// unvalidated over MCP and scales the work of every retrieval stage, so it is
// clamped once, before it fans out.
const maxTopK = 1000

// effectiveTopK applies the configured default to an unset top_k, then clamps
// it to maxTopK.
func effectiveTopK(topK, defaultTopK int) int {
	if topK <= 0 {
		topK = defaultTopK
	}
	return min(topK, maxTopK)
}

type Searcher struct {
	store     *store.Store
	client    *embed.Client
	llmClient *llm.Client
	cfg       *config.Config
}

func NewSearcher(s *store.Store, c *embed.Client, cfg *config.Config, llmClient *llm.Client) *Searcher {
	return &Searcher{store: s, client: c, cfg: cfg, llmClient: llmClient}
}

// retrievalTask is one query text to retrieve for. Query expansion turns a
// single sub-query into several, and each of those runs a vector search, an
// FTS search, or both. Collecting them up front lets every embedding go out in
// one API call and every database query run at the same time, instead of one
// round trip and one index scan at a time.
type retrievalTask struct {
	text    string
	source  querySource
	filters MetadataFilters
	wantVec bool
	wantFTS bool
	vec     []float32
}

// planRetrieval expands each sub-query and flattens the result into tasks.
func (s *Searcher) planRetrieval(ctx context.Context, subQueries []SubQuery) []retrievalTask {
	var tasks []retrievalTask
	for _, sq := range subQueries {
		textQueries := []string{sq.Text}
		if sq.Text != "" && sq.Mode != "fts" && s.cfg.Search.QueryExpansion && s.llmClient != nil {
			if expanded := expandQuery(ctx, s.llmClient, sq.Text); len(expanded) > 0 {
				textQueries = expanded
			}
		}

		for i, q := range textQueries {
			source := querySourceExpanded
			if i == 0 {
				source = querySourceOriginal
			}
			tasks = append(tasks, retrievalTask{
				text:    q,
				source:  source,
				filters: sq.Filters,
				wantVec: sq.Mode != "fts" && strings.TrimSpace(q) != "",
				wantFTS: sq.Mode != "vec",
			})
		}
	}
	return tasks
}

// embedTasks embeds every task that needs a vector in one batch request. A
// batch failure drops vector search for the whole query, where per-query
// embedding used to lose only one variant; in practice the embedding endpoint
// fails for all of them or none.
func (s *Searcher) embedTasks(ctx context.Context, tasks []retrievalTask) error {
	texts := make([]string, 0, len(tasks))
	idx := make([]int, 0, len(tasks))
	for i := range tasks {
		if tasks[i].wantVec {
			texts = append(texts, tasks[i].text)
			idx = append(idx, i)
		}
	}
	if len(texts) == 0 {
		return nil
	}

	embedCtx, cancel := context.WithTimeout(ctx, s.cfg.Search.EmbeddingTimeout)
	defer cancel()

	vecs, err := s.client.EmbedBatch(embedCtx, texts)
	if err != nil {
		slog.Warn("embedding failed", "error", err, "queries", len(texts))
		for _, i := range idx {
			tasks[i].wantVec = false
		}
		return err
	}
	for n, i := range idx {
		tasks[i].vec = vecs[n]
	}
	return nil
}

// dbQuerySem bounds how many index queries run at once. Both a vec0 KNN scan
// and an FTS5 match are CPU-bound, so more of them than cores only adds
// contention and SQLite connections.
//
// The bound sits on the queries themselves rather than on the retrieval tasks,
// because one task is no longer one query: a vector search over an index with
// several sources runs a scan per source. Counting tasks let a single search
// put four times the intended number of scans on the CPU. Holding a slot for a
// task while its scans waited for slots of their own would deadlock, so only
// the queries acquire.
var dbQuerySem = make(chan struct{}, max(runtime.GOMAXPROCS(0), 1))

func acquireDBQuery() { dbQuerySem <- struct{}{} }
func releaseDBQuery() { <-dbQuerySem }

// runRetrieval executes every task's searches concurrently and returns the
// result sets in task order, so fusion does not depend on completion order.
func (s *Searcher) runRetrieval(tasks []retrievalTask, topK int, folders []string) (
	sets []weightedResultSet, vecSetCount, ftsSetCount int, vecErr, ftsErr error,
) {
	type taskResult struct {
		vec, fts       []Result
		vecErr, ftsErr error
	}
	out := make([]taskResult, len(tasks))

	var wg sync.WaitGroup
	run := func(fn func()) { wg.Go(fn) }

	for i := range tasks {
		task := tasks[i]
		if task.wantVec {
			run(func() {
				blob := store.Float32sToBlob(task.vec)
				out[i].vec, out[i].vecErr = searchVec(
					s.store, blob, topK, folders, task.filters, s.cfg.Search.SimilarityThreshold)
			})
		}
		if task.wantFTS {
			run(func() {
				out[i].fts, out[i].ftsErr = searchFTS(s.store, task.text, topK, folders, task.filters)
			})
		}
	}
	wg.Wait()

	for i := range out {
		if out[i].vecErr != nil {
			slog.Warn("vec search failed", "error", out[i].vecErr)
			vecErr = out[i].vecErr
		} else if len(out[i].vec) > 0 {
			sets = append(sets, weightedResultSet{
				Results:  out[i].vec,
				Modality: retrievalModalityVec,
				Source:   tasks[i].source,
			})
			vecSetCount++
		}
		if out[i].ftsErr != nil {
			slog.Warn("FTS search failed", "error", out[i].ftsErr)
			ftsErr = out[i].ftsErr
		} else if len(out[i].fts) > 0 {
			sets = append(sets, weightedResultSet{
				Results:  out[i].fts,
				Modality: retrievalModalityFTS,
				Source:   tasks[i].source,
			})
			ftsSetCount++
		}
	}
	return sets, vecSetCount, ftsSetCount, vecErr, ftsErr
}

// Search runs hybrid retrieval: a vector search over embeddings and a keyword
// search over the FTS index, fused with Reciprocal Rank Fusion into one ranked
// list. The query syntax can restrict a sub-query to one modality, and with an
// LLM configured the query is expanded before retrieval and the top results
// are reranked after it.
func (s *Searcher) Search(ctx context.Context, query string, topK int, folders []string) ([]Result, error) {
	topK = effectiveTopK(topK, s.cfg.Search.DefaultTopK)

	subQueries := parseTypedQuery(query)
	if len(subQueries) == 0 {
		return nil, nil
	}

	tasks := s.planRetrieval(ctx, subQueries)
	anyVecErr := s.embedTasks(ctx, tasks)
	allSets, vecSetCount, ftsSetCount, runVecErr, anyFtsErr := s.runRetrieval(tasks, topK, folders)
	if runVecErr != nil {
		anyVecErr = runVecErr
	}

	// If all backends failed, surface the error
	if vecSetCount == 0 && ftsSetCount == 0 {
		if anyVecErr != nil && anyFtsErr != nil {
			return nil, fmt.Errorf("all search backends failed: vec: %w; fts: %w", anyVecErr, anyFtsErr)
		}
		if anyVecErr != nil {
			return nil, fmt.Errorf("search failed (vec failed, no FTS results): %w", anyVecErr)
		}
		if anyFtsErr != nil {
			return nil, fmt.Errorf("search failed (FTS failed, no vec results): %w", anyFtsErr)
		}
	}

	var searchErr error
	if anyVecErr != nil || anyFtsErr != nil {
		searchErr = errors.Join(anyVecErr, anyFtsErr)
	}

	merged := mergeResultSets(allSets, s.cfg.Search.RRFk, topK, fusionWeights{
		Vec:      s.cfg.Search.VecWeight,
		FTS:      s.cfg.Search.FTSWeight,
		Original: s.cfg.Search.OriginalQueryWeight,
		Expanded: s.cfg.Search.ExpandedQueryWeight,
	})

	// Rerank using original query (not expanded variants)
	if s.cfg.Search.Rerank && s.llmClient != nil && len(merged) > 0 {
		topN := s.cfg.Search.RerankTopN
		if topN <= 0 {
			topN = 20
		}
		merged = rerankResults(ctx, s.llmClient, query, merged, topN, rerankBlendConfig{
			TopRetrievalWeight:    s.cfg.Search.RerankTopWeight,
			BottomRetrievalWeight: s.cfg.Search.RerankBottomWeight,
		})
	}

	// Filter results below minimum score threshold
	if s.cfg.Search.MinScore > 0 && len(merged) > 0 {
		before := len(merged)
		merged = filterMinScore(merged, s.cfg.Search.MinScore)
		if dropped := before - len(merged); dropped > 0 {
			slog.Info("min_score filter applied", "threshold", s.cfg.Search.MinScore, "dropped", dropped, "remaining", len(merged))
		}
	}

	// Annotate results with context from folder config
	for i := range merged {
		for _, f := range s.cfg.Folders {
			if merged[i].Folder == f.Path && len(f.Context) > 0 {
				merged[i].Context = ResolveContext(merged[i].FilePath, f.Path, f.Context)
				break
			}
		}
	}

	slog.Info("search complete", "query", query, "sub_queries", len(subQueries), "merged", len(merged))
	return merged, searchErr
}
