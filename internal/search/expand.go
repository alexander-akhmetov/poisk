package search

import (
	"context"
	"log/slog"
	"strings"

	"github.com/akhmetov/poisk/internal/llm"
)

const expandPrompt = `Generate 2-3 alternative search queries for the following query. These should capture different phrasings, synonyms, or related terms that might find relevant code or documents.

Return ONLY the alternative queries, one per line, without numbering or explanations.

Query: %s`

func expandQuery(ctx context.Context, client *llm.Client, original string) ([]string, error) {
	prompt := strings.Replace(expandPrompt, "%s", original, 1)
	resp, err := client.Complete(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, 0.3, 200)
	if err != nil {
		slog.Warn("query expansion failed, using original", "error", err)
		return []string{original}, nil
	}

	variants := []string{original}
	for line := range strings.SplitSeq(resp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == original {
			continue
		}
		variants = append(variants, line)
		if len(variants) >= 4 { // original + 3 variants max
			break
		}
	}

	slog.Info("query expanded", "original", original, "variants", len(variants))
	return variants, nil
}
