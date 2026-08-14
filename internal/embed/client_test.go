package embed //nolint:revive // internal package, no conflict with stdlib embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

	client := NewClient(server.URL, "", "test-model", 3, true, false)
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
	client := NewClient("http://unused", "", "model", 3, true, false)
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

	client := NewClient(server.URL, "", "model", 3, true, false)
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

	client := NewClient(server.URL, "", "test-model", 3, true, false)
	embedding, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(embedding) != 3 {
		t.Fatalf("got %d dimensions, want 3", len(embedding))
	}
	// [0.1 0.2 0.3] L2-normalized
	norm := float32(math.Sqrt(0.1*0.1 + 0.2*0.2 + 0.3*0.3))
	want := []float32{0.1 / norm, 0.2 / norm, 0.3 / norm}
	for i := range want {
		if math.Abs(float64(embedding[i]-want[i])) > 1e-6 {
			t.Errorf("embedding = %v, want %v", embedding, want)
			break
		}
	}
}

func TestEmbedBatchRetries5xx(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{1, 2, 3}, Index: 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model", 3, true)
	emb, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(emb) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(emb))
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
}

func TestEmbedBatchNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model", 3, true)
	_, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call (no retry for 4xx), got %d", got)
	}
}

func TestEmbedBatchExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model", 3, true)
	_, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != int32(maxRetries) {
		t.Errorf("expected %d calls, got %d", maxRetries, got)
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

	client := NewClient(server.URL, "test-key", "model", 3, true, false)
	_, err := client.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedBatchMatryoshkaAndNormalization(t *testing.T) {
	tests := []struct {
		name       string
		matryoshka bool
		dimensions int
		embedding  []float32
		want       []float32 // nil means an error is expected
	}{
		{
			name:       "exact dims normalized",
			matryoshka: false,
			dimensions: 3,
			embedding:  []float32{3, 0, 4},
			want:       []float32{0.6, 0, 0.8},
		},
		{
			name:       "zero vector left as-is",
			matryoshka: false,
			dimensions: 3,
			embedding:  []float32{0, 0, 0},
			want:       []float32{0, 0, 0},
		},
		{
			name:       "matryoshka truncates then renormalizes",
			matryoshka: true,
			dimensions: 2,
			embedding:  []float32{3, 4, 12},
			want:       []float32{0.6, 0.8},
		},
		{
			name:       "matryoshka exact dims accepted",
			matryoshka: true,
			dimensions: 3,
			embedding:  []float32{0, 5, 0},
			want:       []float32{0, 1, 0},
		},
		{
			name:       "longer rejected when matryoshka off",
			matryoshka: false,
			dimensions: 2,
			embedding:  []float32{1, 2, 3},
			want:       nil,
		},
		{
			name:       "shorter rejected when matryoshka on",
			matryoshka: true,
			dimensions: 4,
			embedding:  []float32{1, 2, 3},
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := embeddingResponse{
					Data: []struct {
						Embedding []float32 `json:"embedding"`
						Index     int       `json:"index"`
					}{
						{Embedding: tt.embedding, Index: 0},
					},
				}
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					t.Fatal(err)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "", "model", tt.dimensions, false, tt.matryoshka)
			got, err := client.Embed(context.Background(), "test")
			if tt.want == nil {
				if err == nil {
					t.Fatal("expected dimension mismatch error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d dimensions, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if math.Abs(float64(got[i]-tt.want[i])) > 1e-6 {
					t.Fatalf("embedding = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
