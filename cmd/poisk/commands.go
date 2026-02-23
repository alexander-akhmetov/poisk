package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/index"
	"github.com/akhmetov/poisk/internal/llm"
	mcpserver "github.com/akhmetov/poisk/internal/mcp"
	"github.com/akhmetov/poisk/internal/search"
	"github.com/akhmetov/poisk/internal/store"
)

func cmdServe() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions, cfg.Embedding.SendDimensions)
	indexer := index.NewIndexer(db, client, cfg)

	var llmClient *llm.Client
	if cfg.LLM.BaseURL != "" {
		llmClient = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	}
	searcher := search.NewSearcher(db, client, cfg, llmClient)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting MCP server", "transport", "stdio", "folders", len(cfg.Folders))

	if err := mcpserver.Run(ctx, indexer, searcher, db, cfg); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

func cmdIndex() error {
	watch := false
	interval := 5 * time.Minute
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--watch":
			watch = true
		case "--interval":
			if i+1 >= len(os.Args) {
				return fmt.Errorf("--interval requires a value (e.g. 5m, 30s)")
			}
			i++
			d, err := time.ParseDuration(os.Args[i])
			if err != nil {
				return fmt.Errorf("invalid interval %q: %w", os.Args[i], err)
			}
			interval = d
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions, cfg.Embedding.SendDimensions)
	indexer := index.NewIndexer(db, client, cfg)

	if !watch {
		return indexOnce(indexer)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("watch mode started", "interval", interval)
	if err := indexOnce(indexer); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("watch mode stopped")
			return nil
		case <-ticker.C:
			if err := indexOnce(indexer); err != nil {
				slog.Error("indexing cycle failed", "error", err)
			}
		}
	}
}

func indexOnce(indexer *index.Indexer) error {
	stats, err := indexer.IndexAll(context.Background())
	if err != nil {
		return fmt.Errorf("indexing: %w", err)
	}
	for _, s := range stats {
		fmt.Fprintf(os.Stderr, "%-40s files=%d chunks=%d skipped=%d errors=%d parse_errors=%d\n",
			s.Folder, s.FilesProcessed, s.ChunksCreated, s.FilesSkipped, s.Errors,
			s.FilesSkippedParseError)
	}
	return nil
}

func cmdSearch() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: poisk search <query>")
	}
	query := strings.Join(os.Args[2:], " ")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions, cfg.Embedding.SendDimensions)

	var llmCli *llm.Client
	if cfg.LLM.BaseURL != "" {
		llmCli = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	}
	searcher := search.NewSearcher(db, client, cfg, llmCli)

	ctx := context.Background()
	results, err := searcher.Search(ctx, query, cfg.Search.DefaultTopK, nil)
	if err != nil && len(results) == 0 {
		return fmt.Errorf("search: %w", err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: partial search failure: %v\n", err)
	}

	for _, r := range results {
		fmt.Printf("[%.2f] %s:%d  %s\n", r.Score, r.FilePath, r.LineNum, truncate(r.Text, 100))
	}
	return nil
}

func cmdStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	status := struct {
		Folders      []FolderStatus `json:"folders"`
		VecAvailable bool           `json:"vec_available"`
		FTSAvailable bool           `json:"fts_available"`
	}{
		VecAvailable: db.VecAvailable(),
		FTSAvailable: db.FTSAvailable(),
	}

	for _, f := range cfg.Folders {
		count, _ := db.Count(f.Path)
		fileCount, _ := db.TrackedFileCount(f.Path)
		status.Folders = append(status.Folders, FolderStatus{
			Path:        f.Path,
			Description: f.Description,
			Files:       fileCount,
			Chunks:      count,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(status)
}

type FolderStatus struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Files       int    `json:"files"`
	Chunks      int    `json:"chunks"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
