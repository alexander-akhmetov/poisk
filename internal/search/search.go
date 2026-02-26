package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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

type Searcher struct {
	store     *store.Store
	client    *embed.Client
	llmClient *llm.Client
	cfg       *config.Config
}

func NewSearcher(s *store.Store, c *embed.Client, cfg *config.Config, llmClient *llm.Client) *Searcher {
	return &Searcher{store: s, client: c, cfg: cfg, llmClient: llmClient}
}

//nolint:gocyclo
func (s *Searcher) Search(ctx context.Context, query string, topK int, folders []string) ([]Result, error) {
	if topK <= 0 {
		topK = s.cfg.Search.DefaultTopK
	}

	subQueries := parseTypedQuery(query)
	if len(subQueries) == 0 {
		return nil, nil
	}

	var allSets []weightedResultSet
	var vecSetCount, ftsSetCount int
	var anyVecErr, anyFtsErr error

	for _, sq := range subQueries {
		// Determine which queries to run (with possible expansion)
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

			// Vector search (for hybrid and vec modes)
			if sq.Mode != "fts" && strings.TrimSpace(q) != "" {
				queryVec, err := s.client.Embed(ctx, q)
				if err != nil {
					slog.Warn("embedding failed", "error", err, "query", q)
					anyVecErr = err
				} else {
					blob := store.Float32sToBlob(queryVec)
					vecResults, vecErr := searchVec(s.store, blob, topK, folders, sq.Filters, s.cfg.Search.SimilarityThreshold)
					if vecErr != nil {
						slog.Warn("vec search failed", "error", vecErr)
						anyVecErr = vecErr
					} else if len(vecResults) > 0 {
						allSets = append(allSets, weightedResultSet{
							Results:  vecResults,
							Modality: retrievalModalityVec,
							Source:   source,
						})
						vecSetCount++
					}
				}
			}

			// FTS search (for hybrid and fts modes)
			if sq.Mode != "vec" {
				ftsResults, ftsErr := searchFTS(s.store, q, topK, folders, sq.Filters)
				if ftsErr != nil {
					slog.Warn("FTS search failed", "error", ftsErr)
					anyFtsErr = ftsErr
				} else if len(ftsResults) > 0 {
					allSets = append(allSets, weightedResultSet{
						Results:  ftsResults,
						Modality: retrievalModalityFTS,
						Source:   source,
					})
					ftsSetCount++
				}
			}
		}
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
