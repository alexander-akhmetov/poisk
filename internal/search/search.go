package search

import (
	"context"
	"log/slog"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/store"
)

type Result struct {
	FilePath string
	LineNum  int
	Text     string
	Score    float64
	Folder   string
}

type Searcher struct {
	store  *store.Store
	client *embed.Client
	cfg    *config.Config
}

func NewSearcher(s *store.Store, c *embed.Client, cfg *config.Config) *Searcher {
	return &Searcher{store: s, client: c, cfg: cfg}
}

func (s *Searcher) Search(ctx context.Context, query string, topK int, folder string) ([]Result, error) {
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
	if queryVec != nil {
		blob := store.Float32sToBlob(queryVec)
		vecResults, err = searchVec(s.store, blob, topK, folder, s.cfg.Search.SimilarityThreshold)
		if err != nil {
			slog.Warn("vec search failed", "error", err)
		}
	}

	// FTS search
	ftsResults, err := searchFTS(s.store, query, topK, folder)
	if err != nil {
		slog.Warn("FTS search failed", "error", err)
	}

	return mergeResults(vecResults, ftsResults, s.cfg.Search.VectorWeight, s.cfg.Search.TextWeight, topK), nil
}
