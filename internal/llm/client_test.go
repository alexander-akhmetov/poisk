package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
			Choices: []completionChoice{{Message: completionMessage{Content: "hello world"}}},
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
			Choices: []completionChoice{{Message: completionMessage{Content: "ok"}}},
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

func TestCompleteExtraBody(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		resp := completionResponse{
			Choices: []completionChoice{{Message: completionMessage{Content: "ok"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "model", WithExtraBody(map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"max_tokens":           4096,
	}))
	if _, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10); err != nil {
		t.Fatal(err)
	}

	if gotBody["model"] != "model" {
		t.Errorf("extra_body dropped a standard field: model = %v", gotBody["model"])
	}
	kwargs, ok := gotBody["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs missing or wrong type: %#v", gotBody["chat_template_kwargs"])
	}
	if kwargs["enable_thinking"] != false {
		t.Errorf("enable_thinking = %v, want false", kwargs["enable_thinking"])
	}
	// extra_body wins over the argument so a server that needs a different
	// budget can get one.
	if gotBody["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want 4096", gotBody["max_tokens"])
	}
}

func TestCompleteEmptyContent(t *testing.T) {
	tests := []struct {
		name        string
		choice      completionChoice
		wantErrPart string
	}{
		{
			name: "reasoning only",
			choice: completionChoice{
				FinishReason: "length",
				Message:      completionMessage{ReasoningContent: "thinking..."},
			},
			wantErrPart: "enable_thinking",
		},
		{
			name:        "empty without reasoning",
			choice:      completionChoice{FinishReason: "stop"},
			wantErrPart: `finish_reason="stop"`,
		},
		{
			name:        "whitespace only",
			choice:      completionChoice{FinishReason: "stop", Message: completionMessage{Content: "   \n"}},
			wantErrPart: "empty completion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(completionResponse{Choices: []completionChoice{tt.choice}})
			}))
			defer server.Close()

			client := NewClient(server.URL, "", "model")
			_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10)
			if err == nil {
				t.Fatal("expected an error for empty content")
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Errorf("error %q does not mention %q", err, tt.wantErrPart)
			}
		})
	}
}

func TestThinkingOffByName(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"qwen3", true},
		{"qwen3:8b", true},
		{"Qwen/Qwen3-32B", true},
		{"mac-studio/qwen3.6-35b-a3b", true},
		{"qwen-3-32b", true},
		{"qwen2.5-coder", false},
		{"llama3", false},
		{"gpt-4o", false},
		{"claude-sonnet-5", false},
		{"", false},
		// A model whose name merely ends in something like qwen3 must not match
		// on a substring boundary that is part of another word.
		{"myqwen3000", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := thinkingOffByName(tt.model) != nil
			if got != tt.want {
				t.Errorf("thinkingOffByName(%q) matched = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

// requestRecorder serves a fixed content and records every request body.
type requestRecorder struct {
	*httptest.Server
	mu       sync.Mutex
	bodies   []map[string]any
	rejectCT bool // reject requests carrying chat_template_kwargs with a 400
}

func (r *requestRecorder) recorded() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, len(r.bodies))
	copy(out, r.bodies)
	return out
}

func newRequestRecorder(t *testing.T, rejectCT bool) *requestRecorder {
	t.Helper()
	rec := &requestRecorder{rejectCT: rejectCT}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		rec.mu.Lock()
		rec.bodies = append(rec.bodies, body)
		rec.mu.Unlock()

		if _, has := body["chat_template_kwargs"]; has && rec.rejectCT {
			http.Error(w, `{"error":"Unrecognized request argument: chat_template_kwargs"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(completionResponse{
			Choices: []completionChoice{{Message: completionMessage{Content: "ok"}}},
		})
	}))
	t.Cleanup(rec.Close)
	return rec
}

func thinkingDisabled(body map[string]any) bool {
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	return ok && kwargs["enable_thinking"] == false
}

func TestCompleteDisablesThinkingForKnownModel(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		extraBody map[string]any
		want      bool
	}{
		{name: "qwen3 gets it by default", model: "mac-studio/qwen3.6-35b-a3b", want: true},
		{name: "unknown model is left alone", model: "llama3", want: false},
		{
			name:      "explicit config wins",
			model:     "qwen3:8b",
			extraBody: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRequestRecorder(t, false)
			client := NewClient(rec.URL, "", tt.model, WithExtraBody(tt.extraBody))
			if _, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10); err != nil {
				t.Fatal(err)
			}

			bodies := rec.recorded()
			if len(bodies) != 1 {
				t.Fatalf("made %d requests, want 1", len(bodies))
			}
			if got := thinkingDisabled(bodies[0]); got != tt.want {
				t.Errorf("thinking disabled = %v, want %v (body: %#v)", got, tt.want, bodies[0])
			}
		})
	}
}

// A name match must never break a server that does not know the field.
func TestCompleteDropsThinkingFieldWhenServerRejectsIt(t *testing.T) {
	rec := newRequestRecorder(t, true)
	client := NewClient(rec.URL, "", "qwen3:8b")

	got, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10)
	if err != nil {
		t.Fatalf("first call should recover from the rejection: %v", err)
	}
	if got != "ok" {
		t.Errorf("content = %q, want %q", got, "ok")
	}

	bodies := rec.recorded()
	if len(bodies) != 2 {
		t.Fatalf("made %d requests, want 2 (rejected, then retried without the field)", len(bodies))
	}
	if !thinkingDisabled(bodies[0]) {
		t.Error("first request should have carried the field")
	}
	if thinkingDisabled(bodies[1]) {
		t.Error("retry should have dropped the field")
	}

	// The rejection is remembered, so later searches do not pay for it again.
	if _, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, 10); err != nil {
		t.Fatal(err)
	}
	bodies = rec.recorded()
	if len(bodies) != 3 {
		t.Fatalf("second call made %d extra requests, want 1", len(bodies)-2)
	}
	if thinkingDisabled(bodies[2]) {
		t.Error("field should not be sent again after a rejection")
	}
}
