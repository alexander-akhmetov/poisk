package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/index"
	"github.com/akhmetov/poisk/internal/search"
	"github.com/akhmetov/poisk/internal/store"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context, indexer *index.Indexer, searcher *search.Searcher, db *store.Store, cfg *config.Config) error {
	server := gomcp.NewServer(
		&gomcp.Implementation{Name: "poisk", Version: "0.1.0"},
		nil,
	)

	registerTools(server, indexer, searcher, db, cfg)

	return server.Run(ctx, &gomcp.StdioTransport{})
}

type SearchInput struct {
	Query   string   `json:"query" jsonschema:"Search query text"`
	TopK    int      `json:"top_k,omitempty" jsonschema:"Max results (default 20)"`
	Folders []string `json:"folders,omitempty" jsonschema:"Filter to folder paths"`
}

type ReindexInput struct {
	Folder string `json:"folder,omitempty" jsonschema:"Specific folder or all if empty"`
	Force  bool   `json:"force,omitempty" jsonschema:"Ignore mtimes and full rebuild"`
}

type GetInput struct {
	FilePath  string `json:"file_path" jsonschema:"Path of the indexed file to retrieve"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line to include (1-based inclusive)"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Last line to include (1-based inclusive)"`
}

type MultiGetInput struct {
	Paths    string `json:"paths" jsonschema:"Comma-separated file paths or glob patterns"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Max total bytes to return (default 100000)"`
}

//nolint:gocyclo
func registerTools(server *gomcp.Server, indexer *index.Indexer, searcher *search.Searcher, db *store.Store, cfg *config.Config) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "search",
		Description: "Search indexed source code and documents using hybrid semantic + keyword search. Supports typed queries: 'lex:term' for keyword-only, 'vec:term' for semantic-only, and ' | ' to compose sub-queries (e.g. 'lex:exact | vec:similar'). Metadata filters are supported in any sub-query: language (lang:/language:), kind (kind:/chunk_kind:), and symbol (sym:/symbol:).",
	}, func(ctx context.Context, _ *gomcp.CallToolRequest, args SearchInput) (*gomcp.CallToolResult, any, error) {
		results, err := searcher.Search(ctx, args.Query, args.TopK, args.Folders)
		if err != nil && len(results) == 0 {
			return nil, nil, fmt.Errorf("search: %w", err)
		}

		var sb strings.Builder
		if err != nil {
			fmt.Fprintf(&sb, "WARNING: partial search failure: %v\n\n", err)
		}
		if len(results) == 0 {
			sb.WriteString("No results found.")
		} else {
			for _, r := range results {
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
				fmt.Fprintf(&sb, "[%.2f] %s%s%s\n%s\n\n", r.Score, loc, meta, ctxStr, r.Text)
			}
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "reindex",
		Description: "Re-index configured folders. Optionally specify a single folder or force full rebuild.",
	}, func(ctx context.Context, _ *gomcp.CallToolRequest, args ReindexInput) (*gomcp.CallToolResult, any, error) {
		// Validate folder against configured folders
		if args.Folder != "" {
			valid := false
			for _, f := range cfg.Folders {
				if f.Path == args.Folder {
					valid = true
					break
				}
			}
			if !valid {
				return nil, nil, fmt.Errorf("folder %q not in configured folders", args.Folder)
			}
		}

		if args.Force && args.Folder != "" {
			if err := db.ClearSource(args.Folder); err != nil {
				return nil, nil, fmt.Errorf("clear source %s: %w", args.Folder, err)
			}
		} else if args.Force {
			for _, f := range cfg.Folders {
				if err := db.ClearSource(f.Path); err != nil {
					return nil, nil, fmt.Errorf("clear source %s: %w", f.Path, err)
				}
			}
		}

		var stats []index.FolderStats
		var err error
		if args.Folder != "" {
			s, e := indexer.IndexFolder(ctx, args.Folder)
			stats = []index.FolderStats{s}
			err = e
		} else {
			stats, err = indexer.IndexAll(ctx)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reindex: %w", err)
		}

		var sb strings.Builder
		for _, s := range stats {
			fmt.Fprintf(&sb, "%s: files=%d chunks=%d skipped=%d errors=%d parse_errors=%d\n",
				s.Folder, s.FilesProcessed, s.ChunksCreated, s.FilesSkipped, s.Errors,
				s.FilesSkippedParseError)
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})

	// Tool: get — retrieve a single indexed file
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "get",
		Description: "Retrieve the indexed content of a single file, optionally filtered by line range",
	}, func(_ context.Context, _ *gomcp.CallToolRequest, args GetInput) (*gomcp.CallToolResult, any, error) {
		source := resolveSource(args.FilePath, cfg)
		if source == "" {
			return nil, nil, fmt.Errorf("file %q not under any configured folder", args.FilePath)
		}

		entries, err := db.GetEntriesByPath(source, args.FilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("get entries: %w", err)
		}
		if len(entries) == 0 {
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: "No indexed content found for " + args.FilePath}},
			}, nil, nil
		}

		var sb strings.Builder

		// Write context header if available
		if fc := findFolderConfig(source, cfg); fc != nil && len(fc.Context) > 0 {
			ctxChain := resolveFileContext(args.FilePath, fc)
			if len(ctxChain) > 0 {
				fmt.Fprintf(&sb, "Context: %s\n\n", strings.Join(ctxChain, " > "))
			}
		}

		for _, e := range entries {
			endLine := e.EndLine
			if endLine <= 0 {
				endLine = e.LineNum
			}
			if args.StartLine > 0 && endLine < args.StartLine {
				continue
			}
			if args.EndLine > 0 && e.LineNum > args.EndLine {
				continue
			}
			loc := fmt.Sprintf("%s:%d", e.FilePath, e.LineNum)
			if e.EndLine > 0 && e.EndLine != e.LineNum {
				loc = fmt.Sprintf("%s:%d-%d", e.FilePath, e.LineNum, e.EndLine)
			}
			meta := ""
			if e.Symbol != "" {
				meta = fmt.Sprintf(" [%s]", e.Symbol)
			}
			fmt.Fprintf(&sb, "%s%s\n%s\n\n", loc, meta, e.Text)
		}

		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})

	// Tool: multi_get — retrieve multiple indexed files by path or glob
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "multi_get",
		Description: "Retrieve indexed content for multiple files. Accepts comma-separated paths or glob patterns.",
	}, func(_ context.Context, _ *gomcp.CallToolRequest, args MultiGetInput) (*gomcp.CallToolResult, any, error) {
		maxBytes := args.MaxBytes
		if maxBytes <= 0 {
			maxBytes = 100_000
		}

		patterns := strings.Split(args.Paths, ",")
		for i := range patterns {
			patterns[i] = strings.TrimSpace(patterns[i])
		}

		// Resolve each pattern to file paths
		var filePaths []string
		for _, pat := range patterns {
			if pat == "" {
				continue
			}
			if isGlob(pat) {
				// Match against tracked paths in all folders
				for _, f := range cfg.Folders {
					tracked, err := db.TrackedFilePaths(f.Path)
					if err != nil {
						slog.Warn("multi_get: failed to list tracked paths", "source", f.Path, "error", err)
						continue
					}
					for _, tp := range tracked {
						matched, _ := filepath.Match(pat, tp)
						if !matched {
							// Also try matching against the basename
							matched, _ = filepath.Match(pat, filepath.Base(tp))
						}
						if matched {
							filePaths = append(filePaths, tp)
						}
					}
				}
			} else {
				filePaths = append(filePaths, pat)
			}
		}

		// Deduplicate paths
		seen := make(map[string]bool, len(filePaths))
		deduped := filePaths[:0]
		for _, fp := range filePaths {
			if !seen[fp] {
				seen[fp] = true
				deduped = append(deduped, fp)
			}
		}
		filePaths = deduped

		if len(filePaths) == 0 {
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: "No matching files found."}},
			}, nil, nil
		}

		var sb strings.Builder
		totalBytes := 0
		truncated := false

		for _, fp := range filePaths {
			if truncated {
				break
			}
			source := resolveSource(fp, cfg)
			if source == "" {
				continue
			}

			entries, err := db.GetEntriesByPath(source, fp)
			if err != nil {
				slog.Warn("multi_get: failed to get entries", "file", fp, "error", err)
				continue
			}
			if len(entries) == 0 {
				continue
			}

			header := fmt.Sprintf("=== %s ===\n", fp)
			if fc := findFolderConfig(source, cfg); fc != nil && len(fc.Context) > 0 {
				ctxChain := resolveFileContext(fp, fc)
				if len(ctxChain) > 0 {
					header += fmt.Sprintf("Context: %s\n", strings.Join(ctxChain, " > "))
				}
			}
			if totalBytes+len(header) > maxBytes {
				truncated = true
				break
			}
			sb.WriteString(header)
			totalBytes += len(header)

			for _, e := range entries {
				loc := fmt.Sprintf("%s:%d", e.FilePath, e.LineNum)
				if e.EndLine > 0 && e.EndLine != e.LineNum {
					loc = fmt.Sprintf("%s:%d-%d", e.FilePath, e.LineNum, e.EndLine)
				}
				meta := ""
				if e.Symbol != "" {
					meta = fmt.Sprintf(" [%s]", e.Symbol)
				}
				chunk := fmt.Sprintf("%s%s\n%s\n\n", loc, meta, e.Text)
				if totalBytes+len(chunk) > maxBytes {
					truncated = true
					break
				}
				sb.WriteString(chunk)
				totalBytes += len(chunk)
			}
		}

		if truncated {
			fmt.Fprintf(&sb, "\n[output truncated at %d bytes]\n", maxBytes)
		}

		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})

	// Resource: index status
	server.AddResource(&gomcp.Resource{
		URI:         "poisk://index-status",
		Name:        "Index Status",
		Description: "Current index status including folder stats and feature availability",
		MIMEType:    "application/json",
	}, func(_ context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		status := map[string]any{
			"vec_available": db.VecAvailable(),
			"fts_available": db.FTSAvailable(),
		}

		folders := make([]map[string]any, 0, len(cfg.Folders))
		for _, f := range cfg.Folders {
			count, _ := db.Count(f.Path)
			fileCount, _ := db.TrackedFileCount(f.Path)
			folderInfo := map[string]any{
				"path":        f.Path,
				"description": f.Description,
				"files":       fileCount,
				"chunks":      count,
			}
			if len(f.Context) > 0 {
				folderInfo["context"] = f.Context
			}
			folders = append(folders, folderInfo)
		}
		status["folders"] = folders

		data, _ := json.MarshalIndent(status, "", "  ")
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{{
				URI:      "poisk://index-status",
				MIMEType: "application/json",
				Text:     string(data),
			}},
		}, nil
	})
}

func resolveSource(filePath string, cfg *config.Config) string {
	for _, f := range cfg.Folders {
		if filePath == f.Path || strings.HasPrefix(filePath, f.Path+"/") {
			return f.Path
		}
	}
	return ""
}

func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func findFolderConfig(source string, cfg *config.Config) *config.FolderConfig {
	for i := range cfg.Folders {
		if cfg.Folders[i].Path == source {
			return &cfg.Folders[i]
		}
	}
	return nil
}

func resolveFileContext(filePath string, fc *config.FolderConfig) []string {
	return search.ResolveContext(filePath, fc.Path, fc.Context)
}
