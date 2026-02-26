package chunk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChunkSession_BasicTurn(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"How do I write tests in Go?"},"slug":"graceful-questing-music"}
{"type":"assistant","message":{"role":"assistant","content":"Use the testing package and run go test."}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	c := chunks[0]
	if c.Language != "session" {
		t.Errorf("Language = %q, want %q", c.Language, "session")
	}
	if c.Kind != "turn" {
		t.Errorf("Kind = %q, want %q", c.Kind, "turn")
	}
	if c.Symbol != "graceful-questing-music#1" {
		t.Errorf("Symbol = %q, want %q", c.Symbol, "graceful-questing-music#1")
	}
	if !strings.Contains(c.Text, "User: How do I write tests in Go?") {
		t.Errorf("chunk text missing user question: %q", c.Text)
	}
	if !strings.Contains(c.Text, "Assistant: Use the testing package") {
		t.Errorf("chunk text missing assistant response: %q", c.Text)
	}
	if c.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", c.StartLine)
	}
	if c.EndLine != 2 {
		t.Errorf("EndLine = %d, want 2", c.EndLine)
	}
}

func TestChunkSession_MultiTurn(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"What is Go?"},"slug":"test-session"}
{"type":"assistant","message":{"role":"assistant","content":"Go is a programming language created at Google."}}
{"type":"user","message":{"role":"user","content":"How do I install it?"}}
{"type":"assistant","message":{"role":"assistant","content":"Download from golang.org and follow the install guide."}}
{"type":"user","message":{"role":"user","content":"What about modules?"}}
{"type":"assistant","message":{"role":"assistant","content":"Run go mod init to create a module. Then go mod tidy to manage dependencies."}}`

	chunks := chunkSession(content)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.Language != "session" {
			t.Errorf("chunk[%d].Language = %q, want %q", i, c.Language, "session")
		}
		if c.Kind != "turn" {
			t.Errorf("chunk[%d].Kind = %q, want %q", i, c.Kind, "turn")
		}
	}

	if chunks[0].Symbol != "test-session#1" {
		t.Errorf("chunks[0].Symbol = %q, want %q", chunks[0].Symbol, "test-session#1")
	}
	if chunks[1].Symbol != "test-session#2" {
		t.Errorf("chunks[1].Symbol = %q, want %q", chunks[1].Symbol, "test-session#2")
	}
	if chunks[2].Symbol != "test-session#3" {
		t.Errorf("chunks[2].Symbol = %q, want %q", chunks[2].Symbol, "test-session#3")
	}

	// Verify line numbers are sequential and non-overlapping.
	if chunks[0].StartLine != 1 {
		t.Errorf("chunks[0].StartLine = %d, want 1", chunks[0].StartLine)
	}
	if chunks[0].EndLine != 2 {
		t.Errorf("chunks[0].EndLine = %d, want 2", chunks[0].EndLine)
	}
	if chunks[1].StartLine != 3 {
		t.Errorf("chunks[1].StartLine = %d, want 3", chunks[1].StartLine)
	}
	if chunks[1].EndLine != 4 {
		t.Errorf("chunks[1].EndLine = %d, want 4", chunks[1].EndLine)
	}
	if chunks[2].StartLine != 5 {
		t.Errorf("chunks[2].StartLine = %d, want 5", chunks[2].StartLine)
	}
	if chunks[2].EndLine != 6 {
		t.Errorf("chunks[2].EndLine = %d, want 6", chunks[2].EndLine)
	}
}

func TestChunkSession_ContentAsBlockArray(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Explain generics"}]},"slug":"block-session"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Generics let you write type-parameterized code."}]}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "Explain generics") {
		t.Errorf("missing user text from block array content: %q", chunks[0].Text)
	}
	if !strings.Contains(chunks[0].Text, "Generics let you write") {
		t.Errorf("missing assistant text from block array content: %q", chunks[0].Text)
	}
}

func TestChunkSession_SkipsNonTextContent(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"Fix the bug"},"slug":"skip-test"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"Let me analyze the code..."},{"type":"text","text":"I found the issue in main.go."},{"type":"tool_use","name":"read_file","input":{"path":"main.go"}}]}}
{"type":"tool_result","message":{"role":"tool","content":"file contents here"}}
{"type":"user","message":{"role":"user","content":"Thanks!"}}
{"type":"assistant","message":{"role":"assistant","content":"You're welcome!"}}`

	chunks := chunkSession(content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// First turn: only text block, not thinking or tool_use.
	if strings.Contains(chunks[0].Text, "Let me analyze") {
		t.Error("chunk should not contain thinking block text")
	}
	if strings.Contains(chunks[0].Text, "tool_use") {
		t.Error("chunk should not contain tool_use content")
	}
	if !strings.Contains(chunks[0].Text, "I found the issue") {
		t.Errorf("missing text block content: %q", chunks[0].Text)
	}
}

func TestChunkSession_SkipsProgressAndSystem(t *testing.T) {
	content := `{"type":"system","message":{"role":"system","content":"You are an assistant."}}
{"type":"user","message":{"role":"user","content":"Hello"},"slug":"sys-test"}
{"type":"assistant","message":{"role":"assistant","content":"Hi there! How can I help?"}}
{"type":"progress","message":{"role":"assistant","content":"processing..."}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if strings.Contains(chunks[0].Text, "You are an assistant") {
		t.Error("system message should not appear in chunks")
	}
	if strings.Contains(chunks[0].Text, "processing") {
		t.Error("progress message should not appear in chunks")
	}
}

func TestChunkSession_NoSlug(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"Hello, what is 2+2?"}}
{"type":"assistant","message":{"role":"assistant","content":"Two plus two equals four."}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Symbol != "#1" {
		t.Errorf("Symbol = %q, want %q", chunks[0].Symbol, "#1")
	}
}

func TestChunkSession_NonSessionJSONL(t *testing.T) {
	content := `{"name":"Alice","age":30}
{"name":"Bob","age":25}
{"name":"Charlie","age":35}`

	chunks := chunkSession(content)
	if chunks != nil {
		t.Fatalf("expected nil for non-session JSONL, got %d chunks", len(chunks))
	}
}

func TestChunkSession_MalformedLines(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"What is Go?"},"slug":"malformed-test"}
this is not json at all
{"type":"assistant","message":{"role":"assistant","content":"Go is a language."}}
{"broken json`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk from valid lines, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "What is Go?") {
		t.Errorf("missing user text: %q", chunks[0].Text)
	}
	if !strings.Contains(chunks[0].Text, "Go is a language") {
		t.Errorf("missing assistant text: %q", chunks[0].Text)
	}
}

func TestChunkSession_EmptyFile(t *testing.T) {
	chunks := chunkSession("")
	if chunks != nil {
		t.Fatalf("expected nil for empty file, got %d chunks", len(chunks))
	}
}

func TestChunkSession_TruncatedLastLine(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"Explain channels"},"slug":"trunc-test"}
{"type":"assistant","message":{"role":"assistant","content":"Channels are typed conduits for goroutine communication."}}
{"type":"user","message":{"role":"user","content":"Can you show`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (truncated line skipped), got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "Explain channels") {
		t.Errorf("missing valid turn text: %q", chunks[0].Text)
	}
}

func TestChunkSession_MinLengthFiltering(t *testing.T) {
	// "User: Hi" = 8 chars, well below minChars=20.
	content := `{"type":"user","message":{"role":"user","content":"Hi"},"slug":"short-test"}`

	chunks := chunkSession(content)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks (turn too short), got %d", len(chunks))
	}
	// Should return non-nil (recognized session) so File() doesn't fall back to chunkSource.
	if chunks == nil {
		t.Fatal("expected non-nil empty slice for recognized session with filtered turns")
	}
}

func TestChunkSession_LargeTurnSplit(t *testing.T) {
	// Build a large assistant response that exceeds maxSessionChunkChars.
	paragraphs := make([]string, 0, 20)
	for range 20 {
		paragraphs = append(paragraphs, strings.Repeat("This is paragraph content that fills space. ", 10))
	}
	largeResponse := strings.Join(paragraphs, "\n\n")

	// JSON-encode the response to handle newlines properly.
	encodedResponse, _ := json.Marshal(largeResponse)
	// Strip surrounding quotes since we embed it inside a JSON string field.
	inner := string(encodedResponse[1 : len(encodedResponse)-1])

	content := `{"type":"user","message":{"role":"user","content":"Give me a detailed explanation of Go concurrency patterns."},"slug":"large-turn"}
{"type":"assistant","message":{"role":"assistant","content":"` + inner + `"}}`

	chunks := chunkSession(content)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large turn, got %d", len(chunks))
	}

	// All chunks should have session metadata.
	for i, c := range chunks {
		if c.Language != "session" {
			t.Errorf("chunks[%d].Language = %q, want %q", i, c.Language, "session")
		}
		if c.Kind != "turn" {
			t.Errorf("chunks[%d].Kind = %q, want %q", i, c.Kind, "turn")
		}
		if !strings.HasPrefix(c.Symbol, "large-turn#1") {
			t.Errorf("chunks[%d].Symbol = %q, want prefix %q", i, c.Symbol, "large-turn#1")
		}
	}

	// Distinct StartLine values and valid line ranges.
	seen := map[int]bool{}
	for i, c := range chunks {
		if seen[c.StartLine] {
			t.Errorf("chunks[%d].StartLine = %d is duplicate (FTS dedupe collision)", i, c.StartLine)
		}
		seen[c.StartLine] = true
		if c.EndLine < c.StartLine {
			t.Errorf("chunks[%d].EndLine (%d) < StartLine (%d)", i, c.EndLine, c.StartLine)
		}
	}

	// First chunk should contain the user question.
	if !strings.Contains(chunks[0].Text, "User:") {
		t.Errorf("first split chunk should contain the user question: %q", chunks[0].Text[:min(100, len(chunks[0].Text))])
	}
}

func TestChunkSession_LargeSingleParagraph(t *testing.T) {
	// A single paragraph with no \n\n breaks that exceeds maxSessionChunkChars.
	longText := strings.Repeat("word ", 800) // 4000 chars
	encoded, _ := json.Marshal(longText)
	inner := string(encoded[1 : len(encoded)-1])

	content := `{"type":"user","message":{"role":"user","content":"Explain everything."},"slug":"mono-para"}
{"type":"assistant","message":{"role":"assistant","content":"` + inner + `"}}`

	chunks := chunkSession(content)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for oversized single paragraph, got %d", len(chunks))
	}

	seen := map[int]bool{}
	for i, c := range chunks {
		if c.Language != "session" {
			t.Errorf("chunks[%d].Language = %q, want %q", i, c.Language, "session")
		}
		if seen[c.StartLine] {
			t.Errorf("chunks[%d].StartLine = %d is duplicate", i, c.StartLine)
		}
		seen[c.StartLine] = true
		if c.EndLine < c.StartLine {
			t.Errorf("chunks[%d].EndLine (%d) < StartLine (%d)", i, c.EndLine, c.StartLine)
		}
	}
}

func TestChunkSession_PreFirstUserAssistantDropped(t *testing.T) {
	// Assistant messages before the first user message should be dropped.
	content := `{"type":"assistant","message":{"role":"assistant","content":"System context injection stuff."}}
{"type":"user","message":{"role":"user","content":"What is Go?"},"slug":"pre-user-test"}
{"type":"assistant","message":{"role":"assistant","content":"Go is a programming language."}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if strings.Contains(chunks[0].Text, "System context injection") {
		t.Error("pre-first-user assistant text should be dropped")
	}
}

func TestChunkSession_ConsecutiveUserMessages(t *testing.T) {
	// Two user messages in a row: each starts a new turn.
	content := `{"type":"user","message":{"role":"user","content":"First question about Go interfaces."},"slug":"consec-test"}
{"type":"user","message":{"role":"user","content":"Actually, tell me about channels instead."}}
{"type":"assistant","message":{"role":"assistant","content":"Channels are Go's mechanism for goroutine communication."}}`

	chunks := chunkSession(content)
	// First user message has no assistant reply → too short (just "User: First question...").
	// Second user + assistant → one turn.
	// The first user-only "turn" may or may not pass min length.
	// At minimum, the second turn should exist.
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "channels instead") && strings.Contains(c.Text, "goroutine communication") {
			found = true
		}
	}
	if !found {
		t.Error("expected a chunk containing the second user+assistant turn")
	}
}

func TestChunkSession_ViaFileDispatch(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"How do I write tests in Go?"},"slug":"dispatch-test"}
{"type":"assistant","message":{"role":"assistant","content":"Use the testing package with functions starting with Test."}}`

	chunks, err := File("session.jsonl", content)
	if err != nil {
		t.Fatalf("File() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk via File(), got %d", len(chunks))
	}
	if chunks[0].Language != "session" {
		t.Errorf("Language = %q, want %q", chunks[0].Language, "session")
	}
}

func TestChunkSession_NonSessionJSONLFallsBackViaFile(t *testing.T) {
	content := `{"name":"Alice","age":30}
{"name":"Bob","age":25}
{"name":"Charlie","age":35}`

	chunks, err := File("data.jsonl", content)
	if err != nil {
		t.Fatalf("File() error: %v", err)
	}
	// Should fall back to chunkSource, not return nil.
	if len(chunks) == 0 {
		t.Fatal("expected fallback source chunks for non-session JSONL")
	}
	// Should NOT have session metadata.
	for i, c := range chunks {
		if c.Language == "session" {
			t.Errorf("chunks[%d] should not have Language=session from fallback", i)
		}
	}
}
