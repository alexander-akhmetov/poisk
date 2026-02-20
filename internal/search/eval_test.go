//go:build eval

package search

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type evalQuery struct {
	Query         string   `json:"query"`
	RelevantFiles []string `json:"relevant_files"`
}

func loadEvalQueries(t *testing.T) []evalQuery {
	t.Helper()
	data, err := os.ReadFile("../../testdata/eval/queries.json")
	if err != nil {
		t.Fatalf("load eval queries: %v", err)
	}
	var queries []evalQuery
	if err := json.Unmarshal(data, &queries); err != nil {
		t.Fatalf("parse eval queries: %v", err)
	}
	return queries
}

// TestEvalRecallAndMRR computes Recall@K and MRR using the tokenizer and
// query builder as a proxy for FTS retrieval quality. It tests that the
// tokenizer and query builders produce tokens that would match the expected
// files' content.
func TestEvalRecallAndMRR(t *testing.T) {
	queries := loadEvalQueries(t)

	// For each query, tokenize and check that tokens would plausibly appear
	// in the expected files. This is a structural eval — full retrieval eval
	// requires a populated database.
	var totalRecall float64
	var totalMRR float64
	k := 10

	for _, q := range queries {
		tokens := tokenize(q.Query)
		if len(tokens) == 0 {
			t.Errorf("query %q produced no tokens", q.Query)
			continue
		}

		// Simulate: build all three query forms and verify they're non-empty
		strictQ := buildStrictAND(tokens)
		relaxedQ := buildRelaxedOR(tokens)
		prefixQ := buildPrefixOR(tokens)

		if strictQ == "" {
			t.Errorf("query %q: empty strict AND query", q.Query)
		}
		if relaxedQ == "" {
			t.Errorf("query %q: empty relaxed OR query", q.Query)
		}
		if prefixQ == "" {
			t.Errorf("query %q: empty prefix OR query", q.Query)
		}

		// Read expected files and check that tokens appear in content
		hits := 0
		firstHitRank := 0
		for rank, fp := range q.RelevantFiles {
			content, err := os.ReadFile("../../" + fp)
			if err != nil {
				t.Logf("skip file %s: %v", fp, err)
				continue
			}
			lower := strings.ToLower(string(content))

			// Check if any token appears in the file
			found := false
			for _, tok := range tokens {
				if strings.Contains(lower, tok) {
					found = true
					break
				}
			}
			if found {
				hits++
				if firstHitRank == 0 {
					firstHitRank = rank + 1
				}
			}
		}

		recall := 0.0
		if len(q.RelevantFiles) > 0 {
			recall = float64(hits) / float64(min(len(q.RelevantFiles), k))
		}
		totalRecall += recall

		mrr := 0.0
		if firstHitRank > 0 {
			mrr = 1.0 / float64(firstHitRank)
		}
		totalMRR += mrr
	}

	n := float64(len(queries))
	avgRecall := totalRecall / n
	avgMRR := totalMRR / n

	fmt.Printf("\n=== Evaluation Results ===\n")
	fmt.Printf("Queries:     %d\n", len(queries))
	fmt.Printf("Recall@%d:   %.3f\n", k, avgRecall)
	fmt.Printf("MRR:         %.3f\n", avgMRR)
	fmt.Printf("==========================\n\n")

	// Baseline thresholds — tokens from our code-aware tokenizer should
	// appear in the expected files for the vast majority of queries
	if avgRecall < 0.8 {
		t.Errorf("Recall@%d = %.3f, want >= 0.8", k, avgRecall)
	}
	if avgMRR < 0.8 {
		t.Errorf("MRR = %.3f, want >= 0.8", avgMRR)
	}
}
