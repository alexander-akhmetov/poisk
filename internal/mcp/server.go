package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	Query  string `json:"query" jsonschema:"required,description=Search query text"`
	TopK   int    `json:"top_k,omitempty" jsonschema:"description=Max results (default 20)"`
	Folder string `json:"folder,omitempty" jsonschema:"description=Filter to folder path"`
}

type ReindexInput struct {
	Folder string `json:"folder,omitempty" jsonschema:"description=Specific folder or all if empty"`
	Force  bool   `json:"force,omitempty" jsonschema:"description=Ignore mtimes and full rebuild"`
}

func registerTools(server *gomcp.Server, indexer *index.Indexer, searcher *search.Searcher, db *store.Store, cfg *config.Config) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "search",
		Description: "Search indexed source code and documents using hybrid semantic + keyword search",
	}, func(ctx context.Context, _ *gomcp.CallToolRequest, args SearchInput) (*gomcp.CallToolResult, any, error) {
		results, err := searcher.Search(ctx, args.Query, args.TopK, args.Folder)
		if err != nil {
			return nil, nil, fmt.Errorf("search: %w", err)
		}

		var sb strings.Builder
		if len(results) == 0 {
			sb.WriteString("No results found.")
		} else {
			for _, r := range results {
				fmt.Fprintf(&sb, "[%.2f] %s:%d\n%s\n\n", r.Score, r.FilePath, r.LineNum, r.Text)
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
			db.ClearSource(args.Folder)
		} else if args.Force {
			for _, f := range cfg.Folders {
				db.ClearSource(f.Path)
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
			fmt.Fprintf(&sb, "%s: files=%d chunks=%d skipped=%d errors=%d\n",
				s.Folder, s.FilesProcessed, s.ChunksCreated, s.FilesSkipped, s.Errors)
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

		var folders []map[string]any
		for _, f := range cfg.Folders {
			count, _ := db.Count(f.Path)
			fileCount, _ := db.TrackedFileCount(f.Path)
			folders = append(folders, map[string]any{
				"path":        f.Path,
				"description": f.Description,
				"files":       fileCount,
				"chunks":      count,
			})
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
