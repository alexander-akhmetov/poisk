package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MultiGetInput is the schema for the multi_get tool.
type MultiGetInput struct {
	Paths    string `json:"paths" jsonschema:"Comma-separated file paths or glob patterns"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Max total bytes to return (default 100000, values above 1000000 are capped at 1000000)"`
}

func registerMultiGetTool(server *gomcp.Server, docSvc *app.DocumentService) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "multi_get",
		Description: "Retrieve indexed content for multiple files. Accepts comma-separated paths or glob patterns.",
	}, func(_ context.Context, _ *gomcp.CallToolRequest, args MultiGetInput) (*gomcp.CallToolResult, any, error) {
		results, truncated, err := docSvc.GetMultipleDocuments(args.Paths, args.MaxBytes)
		if err != nil {
			return nil, nil, err
		}
		if len(results) == 0 && !truncated {
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: "No matching files found."}},
			}, nil, nil
		}

		var sb strings.Builder
		for _, dr := range results {
			fmt.Fprintf(&sb, "=== %s ===\n", dr.FilePath)
			if len(dr.Context) > 0 {
				fmt.Fprintf(&sb, "Context: %s\n", strings.Join(dr.Context, " > "))
			}
			for _, c := range dr.Chunks {
				writeChunk(&sb, c)
			}
		}
		if truncated {
			fmt.Fprintf(&sb, "\n[output truncated at %d bytes]\n", app.EffectiveMaxBytes(args.MaxBytes))
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})
}
