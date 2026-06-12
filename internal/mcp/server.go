package mcp

import (
	"context"

	"github.com/alexander-akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newServer builds the MCP server with all tools and resources registered.
func newServer(indexSvc *app.IndexService, searchSvc *app.SearchService, docSvc *app.DocumentService, statusSvc *app.StatusService) *gomcp.Server {
	server := gomcp.NewServer(
		&gomcp.Implementation{Name: "poisk", Version: "0.1.0"},
		nil,
	)

	registerSearchTool(server, searchSvc)
	registerReindexTool(server, indexSvc)
	registerGetTool(server, docSvc)
	registerMultiGetTool(server, docSvc)
	registerStatusResource(server, statusSvc)

	return server
}

// Run starts the MCP server over stdio.
func Run(ctx context.Context, indexSvc *app.IndexService, searchSvc *app.SearchService, docSvc *app.DocumentService, statusSvc *app.StatusService) error {
	return newServer(indexSvc, searchSvc, docSvc, statusSvc).Run(ctx, &gomcp.StdioTransport{})
}
