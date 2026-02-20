package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/index"
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

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	indexer := index.NewIndexer(db, client, cfg)
	searcher := search.NewSearcher(db, client, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := mcpserver.Run(ctx, indexer, searcher, db, cfg); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

func cmdIndex() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	indexer := index.NewIndexer(db, client, cfg)

	ctx := context.Background()
	stats, err := indexer.IndexAll(ctx)
	if err != nil {
		return fmt.Errorf("indexing: %w", err)
	}

	for _, s := range stats {
		fmt.Fprintf(os.Stderr, "%-40s files=%d chunks=%d skipped=%d errors=%d\n",
			s.Folder, s.FilesProcessed, s.ChunksCreated, s.FilesSkipped, s.Errors)
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

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	searcher := search.NewSearcher(db, client, cfg)

	ctx := context.Background()
	results, err := searcher.Search(ctx, query, cfg.Search.DefaultTopK, "")
	if err != nil {
		return fmt.Errorf("search: %w", err)
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
