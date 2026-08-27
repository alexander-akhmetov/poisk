package search

import (
	"testing"
)

func TestMergeResultsBothEmpty(t *testing.T) {
	got := mergeResults(nil, nil, 60, 10)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMergeResultsVecOnly(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.5},
	}
	got := mergeResults(vec, nil, 60, 10)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// RRF: rank 1 → 1/61, rank 2 → 1/62
	// a.go should be first (higher RRF score)
	if got[0].FilePath != "a.go" {
		t.Errorf("first result = %s, want a.go", got[0].FilePath)
	}
	wantFirst := 1.0 / 61.0
	if abs(got[0].Score-wantFirst) > 1e-9 {
		t.Errorf("score = %f, want %f", got[0].Score, wantFirst)
	}
	wantSecond := 1.0 / 62.0
	if abs(got[1].Score-wantSecond) > 1e-9 {
		t.Errorf("score = %f, want %f", got[1].Score, wantSecond)
	}
}

func TestMergeResultsFTSOnly(t *testing.T) {
	fts := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.8},
		{FilePath: "b.go", LineNum: 5, Score: 0.3},
	}
	got := mergeResults(nil, fts, 60, 10)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	wantFirst := 1.0 / 61.0
	if abs(got[0].Score-wantFirst) > 1e-9 {
		t.Errorf("score = %f, want %f", got[0].Score, wantFirst)
	}
}

func TestMergeResultsDedup(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 10, Text: "vec text", Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Text: "vec b", Score: 0.5},
	}
	fts := []Result{
		{FilePath: "a.go", LineNum: 10, Text: "fts text", Score: 0.7},
		{FilePath: "c.go", LineNum: 1, Text: "fts c", Score: 0.3},
	}
	got := mergeResults(vec, fts, 60, 10)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3 (a.go deduped)", len(got))
	}
	// a.go:10 appears in both: vec rank 1 (1/61) + fts rank 1 (1/61) = 2/61
	wantDeduped := 1.0/61.0 + 1.0/61.0
	if got[0].FilePath != "a.go" {
		t.Errorf("first result = %s, want a.go", got[0].FilePath)
	}
	if abs(got[0].Score-wantDeduped) > 1e-9 {
		t.Errorf("deduped score = %f, want %f", got[0].Score, wantDeduped)
	}
}

func TestMergeResultsRRFHandCalculated(t *testing.T) {
	// Hand-calculated RRF with k=60:
	// Vec: [X rank1, Y rank2, Z rank3]
	// FTS: [Y rank1, Z rank2, W rank3]
	// X: vec 1/61 = 0.016393
	// Y: vec 1/62 + fts 1/61 = 0.016129 + 0.016393 = 0.032522
	// Z: vec 1/63 + fts 1/62 = 0.015873 + 0.016129 = 0.032002
	// W: fts 1/63 = 0.015873
	// Order: Y > Z > X > W
	vec := []Result{
		{FilePath: "X", LineNum: 1},
		{FilePath: "Y", LineNum: 1},
		{FilePath: "Z", LineNum: 1},
	}
	fts := []Result{
		{FilePath: "Y", LineNum: 1},
		{FilePath: "Z", LineNum: 1},
		{FilePath: "W", LineNum: 1},
	}
	got := mergeResults(vec, fts, 60, 10)
	if len(got) != 4 {
		t.Fatalf("got %d results, want 4", len(got))
	}

	wantOrder := []string{"Y", "Z", "X", "W"}
	for i, want := range wantOrder {
		if got[i].FilePath != want {
			t.Errorf("position %d: got %s, want %s", i, got[i].FilePath, want)
		}
	}

	// Verify Y score
	wantY := 1.0/62.0 + 1.0/61.0
	if abs(got[0].Score-wantY) > 1e-9 {
		t.Errorf("Y score = %f, want %f", got[0].Score, wantY)
	}
}

func TestMergeResultsTopK(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Score: 0.7},
	}
	got := mergeResults(vec, nil, 60, 2)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// Top 2 by RRF rank order should be a.go and b.go
	if got[0].FilePath != "a.go" || got[1].FilePath != "b.go" {
		t.Errorf("top-K order wrong: got %s, %s", got[0].FilePath, got[1].FilePath)
	}
}

func TestMergeResultsDefaultK(t *testing.T) {
	vec := []Result{
		{FilePath: "a.go", LineNum: 1},
	}
	// rrfK=0 should default to 60
	got := mergeResults(vec, nil, 0, 10)
	wantScore := 1.0 / 61.0
	if abs(got[0].Score-wantScore) > 1e-9 {
		t.Errorf("score with default k: got %f, want %f", got[0].Score, wantScore)
	}
}

func TestMergeResultSetsWeightedContribution(t *testing.T) {
	sets := []weightedResultSet{
		{
			Results:  []Result{{FilePath: "original.go", LineNum: 1}},
			Modality: retrievalModalityVec,
			Source:   querySourceOriginal,
		},
		{
			Results:  []Result{{FilePath: "expanded.go", LineNum: 1}},
			Modality: retrievalModalityFTS,
			Source:   querySourceExpanded,
		},
	}

	got := mergeResultSets(sets, 60, 10, fusionWeights{
		Vec:      1.0,
		FTS:      1.0,
		Original: 1.0,
		Expanded: 0.2,
	})
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].FilePath != "original.go" {
		t.Fatalf("first result = %s, want original.go", got[0].FilePath)
	}

	wantOriginal := 1.0 / 61.0
	wantExpanded := 0.2 / 61.0
	if abs(got[0].Score-wantOriginal) > 1e-9 {
		t.Errorf("original score = %f, want %f", got[0].Score, wantOriginal)
	}
	if abs(got[1].Score-wantExpanded) > 1e-9 {
		t.Errorf("expanded score = %f, want %f", got[1].Score, wantExpanded)
	}
}

func TestMergeResultSetsTieBreakByPath(t *testing.T) {
	sets := []weightedResultSet{
		{
			Results: []Result{
				{FilePath: "b.go", LineNum: 1},
				{FilePath: "a.go", LineNum: 1},
			},
			Modality: retrievalModalityVec,
			Source:   querySourceOriginal,
		},
		{
			Results: []Result{
				{FilePath: "a.go", LineNum: 1},
				{FilePath: "b.go", LineNum: 1},
			},
			Modality: retrievalModalityFTS,
			Source:   querySourceOriginal,
		},
	}

	got := mergeResultSets(sets, 60, 10, neutralFusionWeights())
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].FilePath != "a.go" || got[1].FilePath != "b.go" {
		t.Fatalf("tie order = [%s, %s], want [a.go, b.go]", got[0].FilePath, got[1].FilePath)
	}
}

func TestMergeResultSetsNeutralWeightsCompatible(t *testing.T) {
	vec := [][]Result{
		{
			{FilePath: "a.go", LineNum: 1},
			{FilePath: "b.go", LineNum: 1},
		},
	}
	fts := [][]Result{
		{
			{FilePath: "b.go", LineNum: 1},
			{FilePath: "c.go", LineNum: 1},
		},
	}

	legacy := mergeMultiResults(vec, fts, 60, 10)

	sets := []weightedResultSet{
		{Results: vec[0], Modality: retrievalModalityVec, Source: querySourceOriginal},
		{Results: fts[0], Modality: retrievalModalityFTS, Source: querySourceOriginal},
	}
	weighted := mergeResultSets(sets, 60, 10, neutralFusionWeights())

	if len(legacy) != len(weighted) {
		t.Fatalf("length mismatch: legacy=%d weighted=%d", len(legacy), len(weighted))
	}
	for i := range legacy {
		if legacy[i].FilePath != weighted[i].FilePath || legacy[i].LineNum != weighted[i].LineNum {
			t.Fatalf("result[%d] mismatch: legacy=%s:%d weighted=%s:%d", i, legacy[i].FilePath, legacy[i].LineNum, weighted[i].FilePath, weighted[i].LineNum)
		}
		if abs(legacy[i].Score-weighted[i].Score) > 1e-9 {
			t.Fatalf("score[%d] mismatch: legacy=%f weighted=%f", i, legacy[i].Score, weighted[i].Score)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestMergeResultsUsesStoredRowIdentity(t *testing.T) {
	tests := []struct {
		name string
		vec  []Result
		fts  []Result
		want int
	}{
		{
			name: "one row from both backends fuses once",
			vec:  []Result{{RowID: 7, FilePath: "a.go", LineNum: 10, Text: "vec text"}},
			fts:  []Result{{RowID: 7, FilePath: "a.go", LineNum: 10, Text: "fts text"}},
			want: 1,
		},
		{
			name: "fragments sharing a file and line stay separate",
			vec:  []Result{{RowID: 7, FilePath: "a.go", LineNum: 10, Text: "first half"}},
			fts:  []Result{{RowID: 8, FilePath: "a.go", LineNum: 10, Text: "second half"}},
			want: 2,
		},
		{
			name: "results without a row id still key on file and line",
			vec:  []Result{{FilePath: "a.go", LineNum: 10, Text: "vec text"}},
			fts:  []Result{{FilePath: "a.go", LineNum: 10, Text: "fts text"}},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeResults(tt.vec, tt.fts, 60, 10)
			if len(got) != tt.want {
				t.Fatalf("got %d results, want %d", len(got), tt.want)
			}
			if tt.want == 1 {
				// Both backends ranked it first, so both contributions land.
				want := 2.0 / 61.0
				if abs(got[0].Score-want) > 1e-9 {
					t.Fatalf("fused score = %f, want %f", got[0].Score, want)
				}
			}
		})
	}
}
