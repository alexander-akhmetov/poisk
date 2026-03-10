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

	"github.com/alexander-akhmetov/poisk/internal/domain"
	mcpserver "github.com/alexander-akhmetov/poisk/internal/mcp"
)

func cmdServe() error {
	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting MCP server", "transport", "stdio", "folders", len(d.Cfg.Folders))

	if err := mcpserver.Run(ctx, d.IndexSvc, d.SearchSvc, d.DocumentSvc, d.StatusSvc); err != nil {
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

	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	if !watch {
		return indexOnce(d)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("watch mode started", "interval", interval)
	if err := indexOnce(d); err != nil {
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
			if err := indexOnce(d); err != nil {
				slog.Error("indexing cycle failed", "error", err)
			}
		}
	}
}

func indexOnce(d *deps) error {
	stats, err := d.IndexSvc.IndexAll(context.Background())
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

func cmdRun() error {
	topK := 0
	var folders []string
	var queryParts []string

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--top-k", "--top_k":
			if i+1 >= len(os.Args) {
				return fmt.Errorf("--top-k requires a value")
			}
			i++
			n, err := fmt.Sscanf(os.Args[i], "%d", &topK)
			if n != 1 || err != nil {
				return fmt.Errorf("invalid --top-k value: %s", os.Args[i])
			}
		case "--folders":
			if i+1 >= len(os.Args) {
				return fmt.Errorf("--folders requires a value")
			}
			i++
			for f := range strings.SplitSeq(os.Args[i], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					folders = append(folders, f)
				}
			}
		default:
			queryParts = append(queryParts, os.Args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		return fmt.Errorf("usage: poisk run <query> [--top-k N] [--folders dir1,dir2]")
	}

	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	if topK <= 0 {
		topK = d.Cfg.Search.DefaultTopK
	}

	ctx := context.Background()
	results, err := d.SearchSvc.Search(ctx, query, topK, folders)
	if err != nil && len(results) == 0 {
		return fmt.Errorf("search: %w", err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: partial search failure: %v\n", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for _, r := range results {
		printResult(r)
	}
	return nil
}

func printResult(r domain.SearchResult) {
	loc := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
	if r.EndLine > 0 && r.EndLine != r.LineNum {
		loc = fmt.Sprintf("%s:%d-%d", r.FilePath, r.LineNum, r.EndLine)
	}
	meta := ""
	if r.Symbol != "" {
		meta = fmt.Sprintf(" [%s]", r.Symbol)
	}
	ctxStr := ""
	if len(r.Context) > 0 {
		ctxStr = fmt.Sprintf(" (%s)", strings.Join(r.Context, " > "))
	}
	fmt.Printf("[%.2f] %s%s%s\n%s\n\n", r.Score, loc, meta, ctxStr, r.Text)
}

func cmdStatus() error {
	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	status := d.StatusSvc.GetStatus()

	output := struct {
		Folders      []FolderStatusJSON      `json:"folders"`
		VecAvailable bool                    `json:"vec_available"`
		FTSAvailable bool                    `json:"fts_available"`
		Indexing     []IndexingProgressJSON  `json:"indexing,omitempty"`
	}{
		VecAvailable: status.VecAvailable,
		FTSAvailable: status.FTSAvailable,
	}
	for _, f := range status.Folders {
		output.Folders = append(output.Folders, FolderStatusJSON{
			Path:        f.Path,
			Description: f.Description,
			Files:       f.Files,
			Chunks:      f.Chunks,
		})
	}
	for _, p := range status.Indexing {
		pct := 0.0
		if p.Total > 0 {
			pct = float64(p.Processed) / float64(p.Total) * 100
		}
		output.Indexing = append(output.Indexing, IndexingProgressJSON{
			Folder:     p.Folder,
			Total:      p.Total,
			Processed:  p.Processed,
			Percentage: pct,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// FolderStatusJSON is the JSON representation of folder status in CLI output.
type FolderStatusJSON struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Files       int    `json:"files"`
	Chunks      int    `json:"chunks"`
}

// IndexingProgressJSON is the JSON representation of active indexing progress.
type IndexingProgressJSON struct {
	Folder     string  `json:"folder"`
	Total      int     `json:"total"`
	Processed  int     `json:"processed"`
	Percentage float64 `json:"percentage"`
}
