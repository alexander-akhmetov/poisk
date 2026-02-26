package search

// filterMinScore removes results with Score below the threshold.
// Returns the original slice unmodified when minScore <= 0.
func filterMinScore(results []Result, minScore float64) []Result {
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
