package search

import (
	"fmt"
	"sort"
)

// mergeMultiResults merges multiple vec and FTS result sets using RRF.
// Each set contributes a 1/(k+rank) score; scores accumulate across all sets.
func mergeMultiResults(vecSets, ftsSets [][]Result, rrfK int, topK int) []Result {
	if rrfK <= 0 {
		rrfK = 60
	}

	type merged struct {
		Result
		rrfScore float64
	}

	m := make(map[string]*merged)

	addSet := func(results []Result) {
		for rank, r := range results {
			key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
			score := 1.0 / float64(rrfK+rank+1)
			if existing, ok := m[key]; ok {
				existing.rrfScore += score
			} else {
				m[key] = &merged{Result: r, rrfScore: score}
			}
		}
	}

	for _, set := range vecSets {
		addSet(set)
	}
	for _, set := range ftsSets {
		addSet(set)
	}

	results := make([]Result, 0, len(m))
	for _, v := range m {
		v.Score = v.rrfScore
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

// mergeResults combines vector and FTS results using Reciprocal Rank Fusion.
// RRF score for each document = Σ 1/(k + rank_i) where rank_i is the 1-based
// position from each retriever. This is immune to score-scale mismatches.
func mergeResults(vecResults, ftsResults []Result, rrfK int, topK int) []Result {
	if len(vecResults) == 0 && len(ftsResults) == 0 {
		return nil
	}

	if rrfK <= 0 {
		rrfK = 60
	}

	type merged struct {
		Result
		rrfScore float64
	}

	m := make(map[string]*merged)

	for rank, r := range vecResults {
		key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
		score := 1.0 / float64(rrfK+rank+1) // rank+1 for 1-based
		m[key] = &merged{Result: r, rrfScore: score}
	}

	for rank, r := range ftsResults {
		key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
		score := 1.0 / float64(rrfK+rank+1)
		if existing, ok := m[key]; ok {
			existing.rrfScore += score
		} else {
			m[key] = &merged{Result: r, rrfScore: score}
		}
	}

	results := make([]Result, 0, len(m))
	for _, v := range m {
		v.Score = v.rrfScore
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
