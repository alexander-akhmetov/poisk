package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/akhmetov/poisk/internal/app"
	"github.com/akhmetov/poisk/internal/domain"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReindexInput is the schema for the reindex tool.
type ReindexInput struct {
	Folder string `json:"folder,omitempty" jsonschema:"Specific folder or all if empty"`
	Force  bool   `json:"force,omitempty" jsonschema:"Ignore mtimes and full rebuild"`
}

func registerReindexTool(server *gomcp.Server, indexSvc *app.IndexService) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "reindex",
		Description: "Re-index configured folders. Optionally specify a single folder or force full rebuild.",
	}, func(ctx context.Context, _ *gomcp.CallToolRequest, args ReindexInput) (*gomcp.CallToolResult, any, error) {
		if args.Folder != "" && !indexSvc.ValidateFolder(args.Folder) {
			return nil, nil, fmt.Errorf("folder %q not in configured folders", args.Folder)
		}

		if args.Force {
			if args.Folder != "" {
				if err := indexSvc.ClearSource(args.Folder); err != nil {
					return nil, nil, fmt.Errorf("clear source %s: %w", args.Folder, err)
				}
			} else {
				if err := indexSvc.ClearAllSources(); err != nil {
					return nil, nil, fmt.Errorf("clear all sources: %w", err)
				}
			}
		}

		var stats []domain.FolderStats
		var idxErr error
		if args.Folder != "" {
			s, e := indexSvc.IndexFolder(ctx, args.Folder)
			stats = []domain.FolderStats{s}
			idxErr = e
		} else {
			stats, idxErr = indexSvc.IndexAll(ctx)
		}
		if idxErr != nil {
			return nil, nil, fmt.Errorf("reindex: %w", idxErr)
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
}
