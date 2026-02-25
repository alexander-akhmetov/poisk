package main

import (
	"fmt"

	"github.com/akhmetov/poisk/internal/app"
	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/index"
	"github.com/akhmetov/poisk/internal/llm"
	"github.com/akhmetov/poisk/internal/search"
	"github.com/akhmetov/poisk/internal/store"
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
	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	embedClient := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)

	var llmClient *llm.Client
	if cfg.LLM.BaseURL != "" {
		llmClient = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
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
