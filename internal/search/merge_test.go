package search

import "testing"

func TestMergeResultsBothEmpty(t *testing.T) {
	got := mergeResults(nil, nil, 0.7, 0.3, 10)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMergeResultsVecOnly(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.5},
	}
	got := mergeResults(vec, nil, 0.7, 0.3, 10)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// Score should be scaled by totalWeight
	if got[0].Score != 0.9*1.0 { // 0.7+0.3 = 1.0
		t.Errorf("score = %f, want %f", got[0].Score, 0.9)
	}
}

func TestMergeResultsFTSOnly(t *testing.T) {
	fts := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.8},
	}
	got := mergeResults(nil, fts, 0.7, 0.3, 10)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
}

func TestMergeResultsDedup(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 10, Text: "vec text", Score: 0.9},
	}
	fts := []Result{
		{FilePath: "a.go", LineNum: 10, Text: "fts text", Score: 0.7},
	}
	got := mergeResults(vec, fts, 0.7, 0.3, 10)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 (deduped)", len(got))
	}
	// Score = 0.7*0.9 + 0.3*0.7 = 0.63 + 0.21 = 0.84
	expected := 0.7*0.9 + 0.3*0.7
	if abs(got[0].Score-expected) > 0.001 {
		t.Errorf("merged score = %f, want %f", got[0].Score, expected)
	}
}

func TestMergeResultsTopK(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Score: 0.7},
	}
	got := mergeResults(vec, nil, 0.7, 0.3, 2)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
