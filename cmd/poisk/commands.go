package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/domain"
	mcpserver "github.com/alexander-akhmetov/poisk/internal/mcp"
)

func cmdServe() error {
	useHTTP := false
	listen := ""
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--http":
			useHTTP = true
		case "--listen":
			if i+1 >= len(os.Args) {
				return fmt.Errorf("--listen requires an address (e.g. 127.0.0.1:8765)")
			}
			i++
			listen = os.Args[i]
		}
	}

	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if useHTTP {
		addr := d.Cfg.Server.Listen
		if listen != "" {
			addr = listen
		}
		token := d.Cfg.Server.Token
		if token == "" {
			token = os.Getenv("POISK_SERVER_TOKEN")
		}

		slog.Info("starting MCP server", "transport", "http", "addr", addr, "folders", len(d.Cfg.Folders))

		if err := mcpserver.RunHTTP(ctx, addr, token, d.IndexSvc, d.SearchSvc, d.DocumentSvc, d.StatusSvc); err != nil {
			return fmt.Errorf("mcp server: %w", err)
		}
		return nil
	}

	if listen != "" {
		slog.Warn("--listen has no effect without --http")
	}

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if !watch {
		return indexOnce(ctx, d)
	}

	slog.Info("watch mode started", "interval", interval)
	if err := indexOnce(ctx, d); err != nil {
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
			if err := indexOnce(ctx, d); err != nil {
				slog.Error("indexing cycle failed", "error", err)
			}
		}
	}
}

func indexOnce(ctx context.Context, d *deps) error {
	stats, err := d.IndexSvc.IndexAll(ctx)
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

// parseTopK parses a --top-k argument. It rejects anything that is not a whole
// non-negative number rather than falling back to a default, so a malformed
// value never runs a search the user did not ask for.
func parseTopK(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid --top-k value: %s", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid --top-k value: %s (must not be negative)", raw)
	}
	return n, nil
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
			n, err := parseTopK(os.Args[i])
			if err != nil {
				return err
			}
			topK = n
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
	jsonOutput := false
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--json" {
			jsonOutput = true
		}
	}

	d, err := bootstrap()
	if err != nil {
		return err
	}
	defer d.Close()

	status := d.StatusSvc.GetStatus()

	if jsonOutput {
		return printStatusJSON(status)
	}
	printStatusHuman(status)
	return nil
}

func printStatusJSON(status domain.IndexStatus) error {
	output := struct {
		Folders      []FolderStatusJSON     `json:"folders"`
		VecAvailable bool                   `json:"vec_available"`
		FTSAvailable bool                   `json:"fts_available"`
		Indexing     []IndexingProgressJSON `json:"indexing,omitempty"`
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

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

func printStatusHuman(status domain.IndexStatus) {
	// Features
	vec := colorRed + "no" + colorReset
	if status.VecAvailable {
		vec = colorGreen + "yes" + colorReset
	}
	fts := colorRed + "no" + colorReset
	if status.FTSAvailable {
		fts = colorGreen + "yes" + colorReset
	}
	fmt.Printf("Vector search: %s\n", vec)
	fmt.Printf("FTS search:    %s\n", fts)

	// Indexing progress (build lookup)
	progressByFolder := make(map[string]domain.IndexingProgress)
	for _, p := range status.Indexing {
		progressByFolder[p.Folder] = p
	}

	// Folders
	fmt.Printf("\n%sFolders (%d):%s\n", colorBold, len(status.Folders), colorReset)
	for _, f := range status.Folders {
		desc := ""
		if f.Description != "" {
			desc = colorDim + " — " + f.Description + colorReset
		}
		fmt.Printf("  %s%s%s%s\n", colorCyan, f.Path, colorReset, desc)
		fmt.Printf("    %d files, %d chunks\n", f.Files, f.Chunks)

		if p, ok := progressByFolder[f.Path]; ok {
			pct := 0.0
			if p.Total > 0 {
				pct = float64(p.Processed) / float64(p.Total) * 100
			}
			fmt.Printf("    %sindexing: %d/%d files (%.1f%%)%s\n", colorYellow, p.Processed, p.Total, pct, colorReset)
		}
	}
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
