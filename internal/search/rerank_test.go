package search

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/llm"
)

func TestParseScores(t *testing.T) {
	tests := []struct {
		name     string
		resp     string
		expected int
		want     []float64
	}{
		{"valid", "8,3,7", 3, []float64{8, 3, 7}},
		{"json array", "[8,3,7]", 3, []float64{8, 3, 7}},
		{"json object", `{"scores":[8,3,7]}`, 3, []float64{8, 3, 7}},
		{"json fenced", "```json\n[8,3,7]\n```", 3, []float64{8, 3, 7}},
		{"indexed noisy", "1: 8\n2: 3\n3: 7", 3, []float64{8, 3, 7}},
		{"prefixed csv", "scores for 3 docs: 8,3,7", 3, []float64{8, 3, 7}},
		{"numeric prefixed csv", "3 docs: 8,3,7", 3, []float64{8, 3, 7}},
		{"numbered csv list", "1. 8, 2. 7, 3. 6", 3, []float64{8, 7, 6}},
		{"csv with trailing metadata", "8 (confidence 0.9),3,7", 3, []float64{8, 3, 7}},
		{"csv with leading score and metadata field", "7. confidence: 0.9,6,5", 3, []float64{7, 6, 5}},
		{"csv with leading score word and metadata field", "7 confidence: 0.9,6,5", 3, []float64{7, 6, 5}},
		{"numeric with trailing scale text", "8 3 7 out of 10", 3, []float64{8, 3, 7}},
		{"numeric first score equals expected", "3 8 7 out of 10", 3, []float64{3, 8, 7}},
		{"numeric with range preamble", "Scores (0-10): 8 3 7", 3, []float64{8, 3, 7}},
		{"numeric indexed pairs", "1 8 2 7 3 6", 3, []float64{8, 7, 6}},
		{"single score natural language", "1 out of 10", 1, []float64{1}},
		{"single score bare value", "1", 1, []float64{1}},
		{"bare ranking indexes are rejected", "1 2 3", 3, nil},
		{"clamped", "12,-1,5", 3, []float64{10, 0, 5}},
		{"wrong count", "8,3", 3, nil},
		{"short by one at scale", `{"scores":[8,3,7,5,4,6,2,9,1]}`, 10, []float64{8, 3, 7, 5, 4, 6, 2, 9, 1}},
		{"short by two at scale", `{"scores":[8,3,7,5,4,6,2,9]}`, 10, nil},
		{"wrong count extra", "[8,3,7,5]", 3, []float64{8, 3, 7}},
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
	server := newTestLLMServer("[9,2,8]")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	results := []Result{
		{FilePath: "a.go", LineNum: 1, Text: "func A()", Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Text: "func B()", Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Text: "func C()", Score: 0.7},
	}

	reranked := rerankResults(context.Background(), client, "test query", results, 3, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
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
	server := newTestLLMServer(`{"scores":[9,2]}`)
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	results := []Result{
		{FilePath: "a.go", LineNum: 1, Text: "func A()", Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Text: "func B()", Score: 0.8},
		{FilePath: "c.go", LineNum: 1, Text: "func C()", Score: 0.7},
		{FilePath: "d.go", LineNum: 1, Text: "func D()", Score: 0.6},
	}

	reranked := rerankResults(context.Background(), client, "test", results, 2, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
	if len(reranked) != 4 {
		t.Fatalf("got %d results, want 4 (2 reranked + 2 passthrough)", len(reranked))
	}
}

func TestRerankPartialScores(t *testing.T) {
	server := newTestLLMServer(`{"scores":[1,1,1,1,1,1,1,1,9]}`)
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{FilePath: fmt.Sprintf("%d.go", i), LineNum: 1, Score: 1 - float64(i)/10}
	}
	wantUnscored := results[9].Score

	reranked := rerankResults(context.Background(), client, "test", results, 10, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
	if len(reranked) != 10 {
		t.Fatalf("got %d results, want 10", len(reranked))
	}

	pos := make(map[string]int, len(reranked))
	for i, r := range reranked {
		pos[r.FilePath] = i
		if r.FilePath == "9.go" && r.Score != wantUnscored {
			t.Fatalf("unscored result got score %f, want its retrieval score %f", r.Score, wantUnscored)
		}
	}
	if pos["8.go"] > pos["1.go"] {
		t.Fatalf("scored result 8.go ranked below 1.go: %d > %d", pos["8.go"], pos["1.go"])
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

	reranked := rerankResults(context.Background(), client, "test", original, 2, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
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

	reranked := rerankResults(context.Background(), client, "test", original, 2, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
	if reranked[0].FilePath != "a.go" {
		t.Fatalf("expected original order preserved on bad parse")
	}
}

func TestRerankFallbackOnWrongCount(t *testing.T) {
	server := newTestLLMServer("[9]")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	original := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.9},
		{FilePath: "b.go", LineNum: 1, Score: 0.8},
	}

	reranked := rerankResults(context.Background(), client, "test", original, 2, rerankBlendConfig{
		TopRetrievalWeight:    0.8,
		BottomRetrievalWeight: 0.2,
	})
	if reranked[0].FilePath != "a.go" || reranked[1].FilePath != "b.go" {
		t.Fatalf("expected original order preserved on wrong score count")
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{"short", "hello", 500, "hello"},
		{"exact", "abc", 3, "abc"},
		{"truncated", "abcdef", 3, "abc..."},
		{"empty", "", 500, ""},
		{"utf8 multibyte", "héllo wörld", 5, "héllo..."},
		{"chinese", "你好世界测试", 4, "你好世界..."},
		{"emoji", "🎉🎊🎈🎁", 2, "🎉🎊..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRunes(tt.input, tt.maxRunes)
			if got != tt.want {
				t.Fatalf("truncateRunes(%q, %d) = %q, want %q", tt.input, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestSanitizeWeight(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		fallback float64
		want     float64
	}{
		{"normal value", 0.5, 0.8, 0.5},
		{"zero", 0.0, 0.8, 0.0},
		{"one", 1.0, 0.8, 1.0},
		{"negative clamps to zero", -0.5, 0.8, 0.0},
		{"above one clamps to one", 1.5, 0.8, 1.0},
		{"NaN returns fallback", math.NaN(), 0.8, 0.8},
		{"positive Inf returns fallback", math.Inf(1), 0.8, 0.8},
		{"negative Inf returns fallback", math.Inf(-1), 0.8, 0.8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeWeight(tt.value, tt.fallback)
			if got != tt.want {
				t.Fatalf("sanitizeWeight(%v, %v) = %v, want %v", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestRerankStableOrderOnTiedScores(t *testing.T) {
	server := newTestLLMServer("[5,5,5]")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	original := []Result{
		{FilePath: "a.go", LineNum: 1, Score: 0.5},
		{FilePath: "b.go", LineNum: 1, Score: 0.5},
		{FilePath: "c.go", LineNum: 1, Score: 0.5},
	}

	reranked := rerankResults(context.Background(), client, "test", original, 3, rerankBlendConfig{
		TopRetrievalWeight:    0.5,
		BottomRetrievalWeight: 0.5,
	})
	if len(reranked) != 3 {
		t.Fatalf("got %d results, want 3", len(reranked))
	}
	if reranked[0].FilePath != "a.go" || reranked[1].FilePath != "b.go" || reranked[2].FilePath != "c.go" {
		t.Fatalf("expected stable ordering for tied scores, got [%s %s %s]", reranked[0].FilePath, reranked[1].FilePath, reranked[2].FilePath)
	}
}
