package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
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
}

type Searcher struct {
	store  *store.Store
	client *embed.Client
	cfg    *config.Config
}

func NewSearcher(s *store.Store, c *embed.Client, cfg *config.Config) *Searcher {
	return &Searcher{store: s, client: c, cfg: cfg}
}

func (s *Searcher) Search(ctx context.Context, query string, topK int, folders []string) ([]Result, error) {
	if topK <= 0 {
		topK = s.cfg.Search.DefaultTopK
	}

	// Get query embedding
	queryVec, err := s.client.Embed(ctx, query)
	if err != nil {
		slog.Warn("embedding failed, falling back to FTS only", "error", err)
		queryVec = nil
	}

	// Vector search
	var vecResults []Result
	var vecErr error
	if queryVec != nil {
		blob := store.Float32sToBlob(queryVec)
		vecResults, vecErr = searchVec(s.store, blob, topK, folders, s.cfg.Search.SimilarityThreshold)
		if vecErr != nil {
			slog.Warn("vec search failed", "error", vecErr)
		}
	}

	// FTS search
	ftsResults, ftsErr := searchFTS(s.store, query, topK, folders)
	if ftsErr != nil {
		slog.Warn("FTS search failed", "error", ftsErr)
	}

	// If both backends failed, surface the error instead of returning empty results
	if vecErr != nil && ftsErr != nil {
		return nil, fmt.Errorf("all search backends failed: vec: %w; fts: %w", vecErr, ftsErr)
	}
	if queryVec == nil && ftsErr != nil {
		return nil, fmt.Errorf("search failed (embedding unavailable, FTS failed): %w", ftsErr)
	}

	var searchErr error
	if vecErr != nil || ftsErr != nil {
		searchErr = errors.Join(vecErr, ftsErr)
	}

	merged := mergeResults(vecResults, ftsResults, s.cfg.Search.RRFk, topK)
	slog.Info("search complete", "query", query, "vec_results", len(vecResults), "fts_results", len(ftsResults), "merged", len(merged))
	return merged, searchErr
}
