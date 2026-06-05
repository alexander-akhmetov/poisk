package search

import (
	"fmt"
	"sort"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

type retrievalModality string

const (
	retrievalModalityVec retrievalModality = "vec"
	retrievalModalityFTS retrievalModality = "fts"
)

type querySource string

const (
	querySourceOriginal querySource = "original"
	querySourceExpanded querySource = "expanded"
)

type weightedResultSet struct {
	Results  []domain.SearchResult
	Modality retrievalModality
	Source   querySource
}

type fusionWeights struct {
	Vec      float64
	FTS      float64
	Original float64
	Expanded float64
}

func neutralFusionWeights() fusionWeights {
	return fusionWeights{
		Vec:      1.0,
		FTS:      1.0,
		Original: 1.0,
		Expanded: 1.0,
	}
}

func (w fusionWeights) setWeight(modality retrievalModality, source querySource) float64 {
	modalityWeight := 1.0
	switch modality {
	case retrievalModalityVec:
		modalityWeight = w.Vec
	case retrievalModalityFTS:
		modalityWeight = w.FTS
	}

	sourceWeight := 1.0
	switch source {
	case querySourceOriginal:
		sourceWeight = w.Original
	case querySourceExpanded:
		sourceWeight = w.Expanded
	}

	if modalityWeight <= 0 {
		modalityWeight = 1.0
	}
	if sourceWeight <= 0 {
		sourceWeight = 1.0
	}
	return modalityWeight * sourceWeight
}

func mergeResultSets(sets []weightedResultSet, rrfK int, topK int, weights fusionWeights) []domain.SearchResult {
	if len(sets) == 0 {
		return nil
	}
	if rrfK <= 0 {
		rrfK = 60
	}

	type merged struct {
		domain.SearchResult
		rrfScore float64
	}

	m := make(map[string]*merged)

	for _, set := range sets {
		setWeight := weights.setWeight(set.Modality, set.Source)
		for rank, r := range set.Results {
			key := fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
			score := setWeight / float64(rrfK+rank+1)
			if existing, ok := m[key]; ok {
				existing.rrfScore += score
			} else {
				m[key] = &merged{SearchResult: r, rrfScore: score}
			}
		}
	}

	results := make([]domain.SearchResult, 0, len(m))
	for _, v := range m {
		v.Score = v.rrfScore
		results = append(results, v.SearchResult)
	}
	if len(results) == 0 {
		return nil
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].FilePath == results[j].FilePath {
				if results[i].LineNum == results[j].LineNum {
					return results[i].EndLine < results[j].EndLine
				}
				return results[i].LineNum < results[j].LineNum
			}
			return results[i].FilePath < results[j].FilePath
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// mergeMultiResults merges multiple vec and FTS result sets using RRF.
// Each set contributes a 1/(k+rank) score; scores accumulate across all sets.
func mergeMultiResults(vecSets, ftsSets [][]domain.SearchResult, rrfK int, topK int) []domain.SearchResult {
	sets := make([]weightedResultSet, 0, len(vecSets)+len(ftsSets))
	for _, set := range vecSets {
		sets = append(sets, weightedResultSet{
			Results:  set,
			Modality: retrievalModalityVec,
			Source:   querySourceOriginal,
		})
	}
	for _, set := range ftsSets {
		sets = append(sets, weightedResultSet{
			Results:  set,
			Modality: retrievalModalityFTS,
			Source:   querySourceOriginal,
		})
	}

	return mergeResultSets(sets, rrfK, topK, neutralFusionWeights())
}

// mergeResults combines vector and FTS results using Reciprocal Rank Fusion.
// RRF score for each document = Σ 1/(k + rank_i) where rank_i is the 1-based
// position from each retriever. This is immune to score-scale mismatches.
func mergeResults(vecResults, ftsResults []domain.SearchResult, rrfK int, topK int) []domain.SearchResult {
	return mergeResultSets([]weightedResultSet{
		{Results: vecResults, Modality: retrievalModalityVec, Source: querySourceOriginal},
		{Results: ftsResults, Modality: retrievalModalityFTS, Source: querySourceOriginal},
	}, rrfK, topK, neutralFusionWeights())
}
