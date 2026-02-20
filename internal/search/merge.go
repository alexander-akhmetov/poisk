package search

import (
	"fmt"
	"sort"
)

func mergeResults(vecResults, ftsResults []Result, vecWeight, textWeight float64, topK int) []Result {
	if len(vecResults) == 0 && len(ftsResults) == 0 {
		return nil
	}

	totalWeight := vecWeight + textWeight

	// Single source fallback
	if len(vecResults) == 0 {
		for i := range ftsResults {
			ftsResults[i].Score *= totalWeight
		}
		if len(ftsResults) > topK {
			return ftsResults[:topK]
		}
		return ftsResults
	}
	if len(ftsResults) == 0 {
		for i := range vecResults {
			vecResults[i].Score *= totalWeight
		}
		if len(vecResults) > topK {
			return vecResults[:topK]
		}
		return vecResults
	}

	// Merge by file:line key
	type merged struct {
		Result
		vecScore float64
		ftsScore float64
	}

	m := make(map[string]*merged)

	for _, r := range vecResults {
		key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
		m[key] = &merged{Result: r, vecScore: r.Score}
	}

	for _, r := range ftsResults {
		key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
		if existing, ok := m[key]; ok {
			existing.ftsScore = r.Score
		} else {
			m[key] = &merged{Result: r, ftsScore: r.Score}
		}
	}

	results := make([]Result, 0, len(m))
	for _, v := range m {
		v.Score = vecWeight*v.vecScore + textWeight*v.ftsScore
		results = append(results, v.Result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}
