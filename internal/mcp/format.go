package mcp

import (
	"fmt"
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func writeSearchResult(sb *strings.Builder, r domain.SearchResult) {
	loc := formatLocation(r.FilePath, r.LineNum, r.EndLine)
	meta := ""
	if r.Symbol != "" {
		meta = fmt.Sprintf(" [%s]", r.Symbol)
	}
	ctxStr := ""
	if len(r.Context) > 0 {
		ctxStr = fmt.Sprintf(" (%s)", strings.Join(r.Context, " > "))
	}
	fmt.Fprintf(sb, "[%.2f] %s%s%s\n%s\n\n", r.Score, loc, meta, ctxStr, r.Text)
}

func writeChunk(sb *strings.Builder, c domain.Chunk) {
	loc := formatLocation(c.FilePath, c.LineNum, c.EndLine)
	meta := ""
	if c.Symbol != "" {
		meta = fmt.Sprintf(" [%s]", c.Symbol)
	}
	fmt.Fprintf(sb, "%s%s\n%s\n\n", loc, meta, c.Text)
}

func formatLocation(filePath string, lineNum, endLine int) string {
	if endLine > 0 && endLine != lineNum {
		return fmt.Sprintf("%s:%d-%d", filePath, lineNum, endLine)
	}
	return fmt.Sprintf("%s:%d", filePath, lineNum)
}
