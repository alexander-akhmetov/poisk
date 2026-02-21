package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akhmetov/poisk/internal/llm"
)

func newTestLLMServer(response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			}{Content: response}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestExpandQuery(t *testing.T) {
	server := newTestLLMServer("search for code patterns\nfind source code examples")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	variants, err := expandQuery(context.Background(), client, "code search")
	if err != nil {
		t.Fatal(err)
	}

	if len(variants) < 2 {
		t.Fatalf("expected at least 2 variants, got %d", len(variants))
	}
	if variants[0] != "code search" {
		t.Fatalf("first variant should be original, got %q", variants[0])
	}
}

func TestExpandQueryMaxVariants(t *testing.T) {
	server := newTestLLMServer("variant1\nvariant2\nvariant3\nvariant4\nvariant5")
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	variants, err := expandQuery(context.Background(), client, "test query")
	if err != nil {
		t.Fatal(err)
	}

	if len(variants) > 4 {
		t.Fatalf("expected at most 4 variants, got %d", len(variants))
	}
}

func TestExpandQueryFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "", "test")
	variants, err := expandQuery(context.Background(), client, "fallback test")
	if err != nil {
		t.Fatal(err)
	}

	if len(variants) != 1 || variants[0] != "fallback test" {
		t.Fatalf("expected [fallback test], got %v", variants)
	}
}
