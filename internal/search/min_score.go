package search

import "github.com/alexander-akhmetov/poisk/internal/domain"

func filterMinScore(results []domain.SearchResult, minScore float64) []domain.SearchResult {
	if minScore <= 0 || len(results) == 0 {
		return results
	}
	filtered := results[:0]
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
