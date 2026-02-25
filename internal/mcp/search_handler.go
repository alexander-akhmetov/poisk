package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchInput is the schema for the search tool.
type SearchInput struct {
	Query   string   `json:"query" jsonschema:"Search query text"`
	TopK    int      `json:"top_k,omitempty" jsonschema:"Max results (default 20)"`
	Folders []string `json:"folders,omitempty" jsonschema:"Filter to folder paths"`
}

func registerSearchTool(server *gomcp.Server, searchSvc *app.SearchService) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "search",
		Description: "Search indexed source code and documents using hybrid semantic + keyword search. Supports typed queries: 'lex:term' for keyword-only, 'vec:term' for semantic-only, and ' | ' to compose sub-queries (e.g. 'lex:exact | vec:similar'). Metadata filters are supported in any sub-query: language (lang:/language:), kind (kind:/chunk_kind:), and symbol (sym:/symbol:).",
	}, func(ctx context.Context, _ *gomcp.CallToolRequest, args SearchInput) (*gomcp.CallToolResult, any, error) {
		results, err := searchSvc.Search(ctx, args.Query, args.TopK, args.Folders)
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
				writeSearchResult(&sb, r)
			}
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: sb.String()}},
		}, nil, nil
	})
}
