package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Hello world"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	msgs := []Message{{Role: "user", Content: "hi"}}
	got, err := c.Complete(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello world" {
		t.Errorf("got %q, want %q", got, "Hello world")
	}
}

func TestCompleteNoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "test-model")
	got, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

func TestCompleteNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	_, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429: %v", err)
	}
}

func TestCompleteNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	_, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Normal chunk
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()
		// Blank keepalive line
		fmt.Fprint(w, "\n")
		flusher.Flush()
		// Empty delta content
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n")
		flusher.Flush()
		// Another chunk
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n")
		flusher.Flush()
		// Done
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key", "m")
	var parts []string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		parts = append(parts, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(parts, "")
	if got != "Hello world" {
		t.Errorf("got %q, want %q", got, "Hello world")
	}
}

func TestStreamNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestStreamLongSSELine(t *testing.T) {
	longContent := strings.Repeat("x", 50000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", longContent)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	var got string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		got += s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != longContent {
		t.Errorf("got length %d, want %d", len(got), len(longContent))
	}
}

func TestStreamCallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	cbErr := fmt.Errorf("stop")
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		return cbErr
	})
	if err != cbErr {
		t.Errorf("expected callback error, got %v", err)
	}
}

func TestStreamDataNoSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// SSE spec allows "data:" without trailing space
		fmt.Fprint(w, "data:{\"choices\":[{\"delta\":{\"content\":\"no-space\"}}]}\n\n")
		fmt.Fprint(w, "data:[DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	var got string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		got += s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "no-space" {
		t.Errorf("got %q, want %q", got, "no-space")
	}
}

func TestStreamNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m")
	var got string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) error {
		got += s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}
