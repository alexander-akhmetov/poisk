package mcp

import (
	"context"

	"github.com/alexander-akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run starts the MCP server over stdio.
func Run(ctx context.Context, indexSvc *app.IndexService, searchSvc *app.SearchService, docSvc *app.DocumentService, statusSvc *app.StatusService) error {
	server := gomcp.NewServer(
		&gomcp.Implementation{Name: "poisk", Version: "0.1.0"},
		nil,
	)

	registerSearchTool(server, searchSvc)
	registerReindexTool(server, indexSvc)
	registerGetTool(server, docSvc)
	registerMultiGetTool(server, docSvc)
	registerStatusResource(server, statusSvc)

	return server.Run(ctx, &gomcp.StdioTransport{})
}
