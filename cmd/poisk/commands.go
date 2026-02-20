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

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/index"
	mcpserver "github.com/akhmetov/poisk/internal/mcp"
	"github.com/akhmetov/poisk/internal/search"
	"github.com/akhmetov/poisk/internal/store"
)

func cmdServe() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	indexer := index.NewIndexer(db, client, cfg)
	searcher := search.NewSearcher(db, client, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := mcpserver.Run(ctx, indexer, searcher, db, cfg); err != nil {
		slog.Error("mcp server", "error", err)
		os.Exit(1)
	}
}

func cmdIndex() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	indexer := index.NewIndexer(db, client, cfg)

	ctx := context.Background()
	stats, err := indexer.IndexAll(ctx)
	if err != nil {
		slog.Error("indexing failed", "error", err)
		os.Exit(1)
	}

	for _, s := range stats {
		fmt.Fprintf(os.Stderr, "%-40s files=%d chunks=%d skipped=%d errors=%d\n",
			s.Folder, s.FilesProcessed, s.ChunksCreated, s.FilesSkipped, s.Errors)
	}
}

func cmdSearch() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: poisk search <query>\n")
		os.Exit(1)
	}
	query := strings.Join(os.Args[2:], " ")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	client := embed.NewClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model, cfg.Embedding.Dimensions)
	searcher := search.NewSearcher(db, client, cfg)

	ctx := context.Background()
	results, err := searcher.Search(ctx, query, cfg.Search.DefaultTopK, "")
	if err != nil {
		slog.Error("search failed", "error", err)
		os.Exit(1)
	}

	for _, r := range results {
		fmt.Printf("[%.2f] %s:%d  %s\n", r.Score, r.FilePath, r.LineNum, truncate(r.Text, 100))
	}
}

func cmdStatus() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(config.DBPath(), cfg.Embedding.Dimensions)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
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
	enc.Encode(status)
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
