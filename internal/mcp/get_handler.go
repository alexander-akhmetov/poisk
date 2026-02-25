package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetInput is the schema for the get tool.
type GetInput struct {
	FilePath  string `json:"file_path" jsonschema:"Path of the indexed file to retrieve"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line to include (1-based inclusive)"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Last line to include (1-based inclusive)"`
}

func registerGetTool(server *gomcp.Server, docSvc *app.DocumentService) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "get",
		Description: "Retrieve the indexed content of a single file, optionally filtered by line range",
	}, func(_ context.Context, _ *gomcp.CallToolRequest, args GetInput) (*gomcp.CallToolResult, any, error) {
		chunks, breadcrumbs, err := docSvc.GetDocument(args.FilePath, args.StartLine, args.EndLine)
		if err != nil {
			return nil, nil, err
		}
		if len(chunks) == 0 {
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: "No indexed content found for " + args.FilePath}},
			}, nil, nil
		}

		var sb strings.Builder
		if len(breadcrumbs) > 0 {
			fmt.Fprintf(&sb, "Context: %s\n\n", strings.Join(breadcrumbs, " > "))
		}
		for _, c := range chunks {
			writeChunk(&sb, c)
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})
}
