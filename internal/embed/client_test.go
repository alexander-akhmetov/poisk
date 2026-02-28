package embed //nolint:revive // internal package, no conflict with stdlib embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %s, want /embeddings", r.URL.Path)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %s, want test-model", req.Model)
		}

		resp := embeddingResponse{
			Data: make([]struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}, len(req.Input)),
		}
		for i := range req.Input {
			resp.Data[i].Index = i
			resp.Data[i].Embedding = make([]float32, 3)
			resp.Data[i].Embedding[0] = float32(i)
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model", 3, true)
	embeddings, err := client.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddings) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(embeddings))
	}
	if embeddings[0][0] != 0 {
		t.Errorf("first embedding[0] = %f, want 0", embeddings[0][0])
	}
	if embeddings[1][0] != 1 {
		t.Errorf("second embedding[0] = %f, want 1", embeddings[1][0])
	}
}

func TestEmbedBatchEmpty(t *testing.T) {
	client := NewClient("http://unused", "", "model", 3, true)
	embeddings, err := client.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if embeddings != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestEmbedBatchDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{1, 2}, Index: 0}, // wrong dimensions
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model", 3, true)
	_, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Input) != 1 {
			t.Fatalf("expected 1 input, got %d", len(req.Input))
		}
		if req.Input[0] != "hello world" {
			t.Errorf("input = %q, want %q", req.Input[0], "hello world")
		}

		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model", 3, true)
	embedding, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(embedding) != 3 {
		t.Fatalf("got %d dimensions, want 3", len(embedding))
	}
	if embedding[0] != 0.1 || embedding[1] != 0.2 || embedding[2] != 0.3 {
		t.Errorf("embedding = %v, want [0.1 0.2 0.3]", embedding)
	}
}

func TestEmbedBatchAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("auth = %s, want Bearer test-key", auth)
		}
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{1, 2, 3}, Index: 0},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "model", 3, true)
	_, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
}
