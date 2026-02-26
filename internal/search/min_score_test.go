package search

import "testing"

func TestFilterMinScore(t *testing.T) {
	tests := []struct {
		name     string
		results  []Result
		minScore float64
		wantLen  int
	}{
		{
			name:     "disabled when zero",
			results:  []Result{{Score: 0.001}, {Score: 0.0001}},
			minScore: 0,
			wantLen:  2,
		},
		{
			name:     "filters below threshold",
			results:  []Result{{Score: 0.05}, {Score: 0.03}, {Score: 0.01}, {Score: 0.003}},
			minScore: 0.005,
			wantLen:  3,
		},
		{
			name:     "exact threshold passes",
			results:  []Result{{Score: 0.005}, {Score: 0.005}, {Score: 0.001}},
			minScore: 0.005,
			wantLen:  2,
		},
		{
			name:     "all filtered returns empty",
			results:  []Result{{Score: 0.001}, {Score: 0.0005}},
			minScore: 0.01,
			wantLen:  0,
		},
		{
			name:     "empty input unchanged",
			results:  []Result{},
			minScore: 0.01,
			wantLen:  0,
		},
		{
			name:     "negative threshold disabled",
			results:  []Result{{Score: 0.001}},
			minScore: -1,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterMinScore(tt.results, tt.minScore)
			if len(got) != tt.wantLen {
				t.Errorf("filterMinScore() returned %d results, want %d", len(got), tt.wantLen)
			}
		})
	}
}
