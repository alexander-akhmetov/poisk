package search

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/akhmetov/poisk/internal/llm"
)

const rerankPromptTemplate = `Rate the relevance of each document to the query on a 0-10 scale.

Query: QUERY_PLACEHOLDER

Documents:
DOCS_PLACEHOLDER
Return ONLY a comma-separated list of scores (e.g., "8,3,7,5"). One score per document, in order.`

func rerankResults(ctx context.Context, client *llm.Client, query string, results []Result, topN int) []Result {
	if len(results) == 0 {
		return results
	}
	if topN <= 0 || topN > len(results) {
		topN = len(results)
	}

	// Only rerank top N candidates
	candidates := results
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	var docs strings.Builder
	for i, r := range candidates {
		text := r.Text
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		fmt.Fprintf(&docs, "[%d] %s:%d %s\n", i+1, r.FilePath, r.LineNum, text)
	}

	prompt := strings.NewReplacer("QUERY_PLACEHOLDER", query, "DOCS_PLACEHOLDER", docs.String()).Replace(rerankPromptTemplate)
	resp, err := client.Complete(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, 0.0, 100)
	if err != nil {
		slog.Warn("reranking failed, keeping original order", "error", err)
		return results
	}

	scores := parseScores(resp, len(candidates))
	if scores == nil {
		slog.Warn("failed to parse reranker scores, keeping original order", "response", resp)
		return results
	}

	// Position-aware blending: top results keep more retrieval weight
	for i := range candidates {
		// Linear blend: top result gets 80% retrieval / 20% rerank, last gets 20% / 80%
		positionWeight := 0.8 - 0.6*(float64(i)/float64(max(len(candidates)-1, 1)))
		if positionWeight < 0.2 {
			positionWeight = 0.2
		}
		rerankWeight := 1.0 - positionWeight
		candidates[i].Score = positionWeight*candidates[i].Score + rerankWeight*(scores[i]/10.0)
	}

	// Re-merge reranked candidates with remaining results
	reranked := make([]Result, 0, len(results))
	reranked = append(reranked, candidates...)
	if len(results) > topN {
		reranked = append(reranked, results[topN:]...)
	}

	// Sort by final score
	for i := 0; i < len(reranked)-1; i++ {
		for j := i + 1; j < len(reranked); j++ {
			if reranked[j].Score > reranked[i].Score {
				reranked[i], reranked[j] = reranked[j], reranked[i]
			}
		}
	}

	return reranked
}

func parseScores(resp string, expected int) []float64 {
	resp = strings.TrimSpace(resp)
	parts := strings.Split(resp, ",")
	if len(parts) != expected {
		return nil
	}

	scores := make([]float64, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		score, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil
		}
		if score < 0 {
			score = 0
		}
		if score > 10 {
			score = 10
		}
		scores[i] = score
	}
	return scores
}
