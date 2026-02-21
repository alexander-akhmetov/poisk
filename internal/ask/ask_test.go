package ask

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akhmetov/poisk/internal/llm"
	"github.com/akhmetov/poisk/internal/search"
)

type mockSearcher struct {
	results []search.Result
	err     error
}

func (m *mockSearcher) Search(_ context.Context, _ string, _ int, _ []string) ([]search.Result, error) {
	return m.results, m.err
}

func TestBuildMessages(t *testing.T) {
	cases := []struct {
		name         string
		results      []search.Result
		searchErr    error
		systemPrompt string
		question     string
		wantMsgCount int
		wantErr      bool
		checkContent func(t *testing.T, msgs []llm.Message)
	}{
		{
			name: "with results and system prompt",
			results: []search.Result{
				{FilePath: "/a/b.go", LineNum: 10, EndLine: 20, Text: "func foo()", Symbol: "foo"},
				{FilePath: "/c/d.md", LineNum: 5, EndLine: 5, Text: "# Title"},
			},
			systemPrompt: "You are helpful.",
			question:     "what is foo?",
			wantMsgCount: 2,
			checkContent: func(t *testing.T, msgs []llm.Message) {
				if msgs[0].Role != "system" || msgs[0].Content != "You are helpful." {
					t.Errorf("system msg = %+v", msgs[0])
				}
				user := msgs[1].Content
				if !strings.Contains(user, "/a/b.go:10-20 [foo]") {
					t.Errorf("missing location with symbol: %s", user)
				}
				if !strings.Contains(user, "/c/d.md:5") {
					t.Errorf("missing single-line location: %s", user)
				}
				if !strings.Contains(user, "Question: what is foo?") {
					t.Errorf("missing question: %s", user)
				}
			},
		},
		{
			name:         "no system prompt",
			results:      []search.Result{{FilePath: "f", LineNum: 1, Text: "t"}},
			systemPrompt: "",
			question:     "q",
			wantMsgCount: 1,
			checkContent: func(t *testing.T, msgs []llm.Message) {
				if msgs[0].Role != "user" {
					t.Errorf("expected user role, got %s", msgs[0].Role)
				}
			},
		},
		{
			name:         "no results",
			results:      nil,
			question:     "q",
			wantMsgCount: 1,
			checkContent: func(t *testing.T, msgs []llm.Message) {
				if strings.Contains(msgs[0].Content, "Context:") {
					t.Error("should not have context section with no results")
				}
				if !strings.Contains(msgs[0].Content, "Question: q") {
					t.Errorf("missing question: %s", msgs[0].Content)
				}
			},
		},
		{
			name:     "search error with no results",
			searchErr: fmt.Errorf("total failure"),
			question: "q",
			wantErr:  true,
		},
		{
			name:         "partial search error proceeds",
			results:      []search.Result{{FilePath: "f", LineNum: 1, Text: "t"}},
			searchErr:    fmt.Errorf("vec failed"),
			question:     "q",
			wantMsgCount: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms := &mockSearcher{results: tc.results, err: tc.searchErr}
			a := NewAsker(ms, nil, 10, tc.systemPrompt)
			msgs, err := a.buildMessages(context.Background(), tc.question, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(msgs) != tc.wantMsgCount {
				t.Fatalf("expected %d messages, got %d", tc.wantMsgCount, len(msgs))
			}
			if tc.checkContent != nil {
				tc.checkContent(t, msgs)
			}
		})
	}
}

func TestAsk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"answer here"}}]}`)
	}))
	defer srv.Close()

	ms := &mockSearcher{results: []search.Result{{FilePath: "f", LineNum: 1, Text: "ctx"}}}
	c := llm.NewClient(srv.URL, "", "m")
	a := NewAsker(ms, c, 10, "")
	got, err := a.Ask(context.Background(), "what?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "answer here" {
		t.Errorf("got %q, want %q", got, "answer here")
	}
}

func TestAskStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"streamed\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ms := &mockSearcher{results: []search.Result{{FilePath: "f", LineNum: 1, Text: "ctx"}}}
	c := llm.NewClient(srv.URL, "", "m")
	a := NewAsker(ms, c, 10, "")
	var got string
	err := a.AskStream(context.Background(), "what?", nil, func(s string) error {
		got += s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "streamed" {
		t.Errorf("got %q, want %q", got, "streamed")
	}
}
