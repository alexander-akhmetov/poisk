package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akhmetov/poisk/internal/llm"
)

func TestParseScores(t *testing.T) {
	tests := []struct {
		name     string
		resp     string
		expected int
		want     []float64
	}{
		{"valid", "8,3,7", 3, []float64{8, 3, 7}},
		{"with spaces", " 8 , 3 , 7 ", 3, []float64{8, 3, 7}},
		{"clamped", "12,-1,5", 3, []float64{10, 0, 5}},
		{"wrong count", "8,3", 3, nil},
		{"non-numeric", "high,low,mid", 3, nil},
		{"empty", "", 3, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScores(tt.resp, tt.expected)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d scores, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("score[%d] = %f, want %f", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRerankResults(t *testing.T) {
	server := newTestLLMServer("9,2,8")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	results := []Result{
		{FilePath: "a.go", LineNum: 1, Text: "func A()", Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Text: "func B()", Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Text: "func C()", Score: 0.7},
	}

	reranked := rerankResults(context.Background(), client, "test query", results, 3)
	if len(reranked) != 3 {
		t.Fatalf("got %d results, want 3", len(reranked))
	}
	// Results should be reordered by blended score
	for i := 1; i < len(reranked); i++ {
		if reranked[i].Score > reranked[i-1].Score {
			t.Fatalf("results not sorted: %f > %f at index %d", reranked[i].Score, reranked[i-1].Score, i)
		}
	}
}

func TestRerankTopN(t *testing.T) {
	server := newTestLLMServer("9,2")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	results := []Result{
		{FilePath: "a.go", LineNum: 1, Text: "func A()", Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Text: "func B()", Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Text: "func C()", Score: 0.7},
		{FilePath: "d.go", LineNum: 1, Text: "func D()", Score: 0.6},
	}

	reranked := rerankResults(context.Background(), client, "test", results, 2)
	if len(reranked) != 4 {
		t.Fatalf("got %d results, want 4 (2 reranked + 2 passthrough)", len(reranked))
	}
}

func TestRerankFallbackOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	original := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.8},
	}

	reranked := rerankResults(context.Background(), client, "test", original, 2)
	if len(reranked) != 2 {
		t.Fatalf("got %d results, want 2", len(reranked))
	}
	// Should preserve original order
	if reranked[0].FilePath != "a.go" || reranked[1].FilePath != "b.go" {
		t.Fatalf("expected original order preserved")
	}
}

func TestRerankFallbackOnBadParse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		type choice struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		resp := struct {
			Choices []choice `json:"choices"`
		}{
			Choices: []choice{{Message: struct {
				Content string `json:"content"`
			}{Content: "I cannot score these"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	original := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.8},
	}

	reranked := rerankResults(context.Background(), client, "test", original, 2)
	if reranked[0].FilePath != "a.go" {
		t.Fatalf("expected original order preserved on bad parse")
	}
}
