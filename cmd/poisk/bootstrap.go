package main

import (
	"fmt"

	"github.com/alexander-akhmetov/poisk/internal/app"
	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/llm"
	"github.com/alexander-akhmetov/poisk/internal/search"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

type deps struct {
	Cfg *config.Config
	DB  *store.Store

	IndexSvc    *app.IndexService
	SearchSvc   *app.SearchService
	DocumentSvc *app.DocumentService
	StatusSvc   *app.StatusService
}

func (d *deps) Close() error {
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}

// Callers must defer deps.Close().
func bootstrap() (*deps, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return bootstrapWithConfig(cfg)
}

func bootstrapWithConfig(cfg *config.Config) (*deps, error) {
	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions, cfg.Embedding.Quantization)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	embedClient := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
		cfg.Embedding.Matryoshka,
	)

	var llmClient *llm.Client
	if cfg.LLM.BaseURL != "" {
		llmClient = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model,
			llm.WithExtraBody(cfg.LLM.ExtraBody))
	}

	indexer := index.NewIndexer(db, embedClient, cfg)
	searcher := search.NewSearcher(db, embedClient, cfg, llmClient)

	return &deps{
		Cfg: cfg,
		DB:  db,

		IndexSvc:    app.NewIndexService(indexer, db, cfg),
		SearchSvc:   app.NewSearchService(searcher),
		DocumentSvc: app.NewDocumentService(db, cfg),
		StatusSvc:   app.NewStatusService(db, cfg),
	}, nil
}
