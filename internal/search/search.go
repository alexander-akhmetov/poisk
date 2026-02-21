package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/llm"
	"github.com/akhmetov/poisk/internal/store"
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

type Searcher struct {
	store     *store.Store
	client    *embed.Client
	llmClient *llm.Client
	cfg       *config.Config
}

func NewSearcher(s *store.Store, c *embed.Client, cfg *config.Config, llmClient *llm.Client) *Searcher {
	return &Searcher{store: s, client: c, cfg: cfg, llmClient: llmClient}
}

func (s *Searcher) Search(ctx context.Context, query string, topK int, folders []string) ([]Result, error) {
	if topK <= 0 {
		topK = s.cfg.Search.DefaultTopK
	}

	subQueries := parseTypedQuery(query)
	if len(subQueries) == 0 {
		return nil, nil
	}

	var allVecSets [][]Result
	var allFtsSets [][]Result
	var anyVecErr, anyFtsErr error

	for _, sq := range subQueries {
		// Determine which queries to run (with possible expansion)
		textQueries := []string{sq.Text}
		if sq.Mode != "fts" && s.cfg.Search.QueryExpansion && s.llmClient != nil {
			expanded, err := expandQuery(ctx, s.llmClient, sq.Text)
			if err == nil && len(expanded) > 0 {
				textQueries = expanded
			}
		}

		for _, q := range textQueries {
			// Vector search (for hybrid and vec modes)
			if sq.Mode != "fts" {
				queryVec, err := s.client.Embed(ctx, q)
				if err != nil {
					slog.Warn("embedding failed", "error", err, "query", q)
					anyVecErr = err
				} else {
					blob := store.Float32sToBlob(queryVec)
					vecResults, vecErr := searchVec(s.store, blob, topK, folders, s.cfg.Search.SimilarityThreshold)
					if vecErr != nil {
						slog.Warn("vec search failed", "error", vecErr)
						anyVecErr = vecErr
					} else if len(vecResults) > 0 {
						allVecSets = append(allVecSets, vecResults)
					}
				}
			}

			// FTS search (for hybrid and fts modes)
			if sq.Mode != "vec" {
				ftsResults, ftsErr := searchFTS(s.store, q, topK, folders)
				if ftsErr != nil {
					slog.Warn("FTS search failed", "error", ftsErr)
					anyFtsErr = ftsErr
				} else if len(ftsResults) > 0 {
					allFtsSets = append(allFtsSets, ftsResults)
				}
			}
		}
	}

	// If all backends failed, surface the error
	if len(allVecSets) == 0 && len(allFtsSets) == 0 {
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

	// Use single-pair merge for single hybrid query (backward compat), multi merge otherwise
	totalSets := len(allVecSets) + len(allFtsSets)
	var merged []Result
	if totalSets <= 2 && len(subQueries) == 1 && subQueries[0].Mode == "hybrid" {
		var vecResults, ftsResults []Result
		if len(allVecSets) > 0 {
			vecResults = allVecSets[0]
		}
		if len(allFtsSets) > 0 {
			ftsResults = allFtsSets[0]
		}
		merged = mergeResults(vecResults, ftsResults, s.cfg.Search.RRFk, topK)
	} else {
		merged = mergeMultiResults(allVecSets, allFtsSets, s.cfg.Search.RRFk, topK)
	}

	// Rerank using original query (not expanded variants)
	if s.cfg.Search.Rerank && s.llmClient != nil && len(merged) > 0 {
		topN := s.cfg.Search.RerankTopN
		if topN <= 0 {
			topN = 20
		}
		reranked, rerankErr := rerankResults(ctx, s.llmClient, query, merged, topN)
		if rerankErr != nil {
			slog.Warn("reranking failed", "error", rerankErr)
		} else {
			merged = reranked
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
