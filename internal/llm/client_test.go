package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req completionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Model != "test-model" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		resp := completionResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "hello world"}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-model")

	result, err := client.Complete(context.Background(), []Message{
		{Role: "user", Content: "say hello"},
	}, 0.0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Fatalf("got %q, want %q", result, "hello world")
	}
}

func TestCompleteAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := completionResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test123", "model")
	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test123" {
		t.Fatalf("got auth %q, want %q", gotAuth, "Bearer sk-test123")
	}
}

func TestCompleteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model")
	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10)
	if err == nil {
		t.Fatal("expected error")
	}
}
