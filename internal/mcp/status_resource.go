package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alexander-akhmetov/poisk/internal/app"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerStatusResource(server *gomcp.Server, statusSvc *app.StatusService) {
	server.AddResource(&gomcp.Resource{
		URI:         "poisk://index-status",
		Name:        "Index Status",
		Description: "Current index status including folder stats and feature availability",
		MIMEType:    "application/json",
	}, func(_ context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		status := statusSvc.GetStatus()

		output := map[string]any{
			"vec_available": status.VecAvailable,
			"fts_available": status.FTSAvailable,
		}
		folders := make([]map[string]any, 0, len(status.Folders))
		for _, f := range status.Folders {
			folderInfo := map[string]any{
				"path":        f.Path,
				"description": f.Description,
				"files":       f.Files,
				"chunks":      f.Chunks,
			}
			if len(f.Context) > 0 {
				folderInfo["context"] = f.Context
			}
			folders = append(folders, folderInfo)
		}
		output["folders"] = folders

		if len(status.Indexing) > 0 {
			indexing := make([]map[string]any, 0, len(status.Indexing))
			for _, p := range status.Indexing {
				pct := 0.0
				if p.Total > 0 {
					pct = float64(p.Processed) / float64(p.Total) * 100
				}
				indexing = append(indexing, map[string]any{
					"folder":     p.Folder,
					"total":      p.Total,
					"processed":  p.Processed,
					"percentage": pct,
				})
			}
			output["indexing"] = indexing
		}

		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal status: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{{
				URI:      "poisk://index-status",
				MIMEType: "application/json",
				Text:     string(data),
			}},
		}, nil
	})
}
