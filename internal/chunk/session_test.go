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

// --- pi coding-agent format ---

const piHeader = `{"type":"session","version":3,"id":"2df495f5-feee-78b9-8535-9d2a80a81b46","timestamp":"2026-06-01T21:17:51.931Z","cwd":"/Users/alexander/projects/jrnl"}`

func TestChunkSession_PiBasicTurn(t *testing.T) {
	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"hey, does jrnl support pi sessions?"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"Not yet, but the chunker can be extended to parse them."}]}}`

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
	if c.Symbol != "2df495f5#1" {
		t.Errorf("Symbol = %q, want %q", c.Symbol, "2df495f5#1")
	}
	if !strings.Contains(c.Text, "User: hey, does jrnl support pi sessions?") {
		t.Errorf("chunk text missing user question: %q", c.Text)
	}
	if !strings.Contains(c.Text, "Assistant: Not yet") {
		t.Errorf("chunk text missing assistant response: %q", c.Text)
	}
}

func TestChunkSession_PiMultiTurn(t *testing.T) {
	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"What is pi exactly here?"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"pi is a coding agent that records sessions as JSONL."}]}}
{"type":"message","id":"m3","message":{"role":"user","content":[{"type":"text","text":"Where are the sessions stored on disk?"}]}}
{"type":"message","id":"m4","message":{"role":"assistant","content":[{"type":"text","text":"Under the agent sessions directory, one file per session."}]}}`

	chunks := chunkSession(content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Symbol != "2df495f5#1" {
		t.Errorf("chunks[0].Symbol = %q, want %q", chunks[0].Symbol, "2df495f5#1")
	}
	if chunks[1].Symbol != "2df495f5#2" {
		t.Errorf("chunks[1].Symbol = %q, want %q", chunks[1].Symbol, "2df495f5#2")
	}
}

func TestChunkSession_PiStripsThinkingToolCallToolResult(t *testing.T) {
	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"Find the bug in the parser please."}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"thinking","text":"Let me inspect the tokenizer first.","thinkingSignature":"sig"},{"type":"text","text":"The bug is an off-by-one in the loop."},{"type":"toolCall","id":"t1","name":"read_file","arguments":{"path":"parser.go"}}]}}
{"type":"message","id":"m3","message":{"role":"toolResult","content":[{"type":"text","text":"raw file contents from the tool that must not be indexed"}]}}
{"type":"message","id":"m4","message":{"role":"user","content":[{"type":"text","text":"Great, thanks for finding it."}]}}
{"type":"message","id":"m5","message":{"role":"assistant","content":[{"type":"text","text":"You are welcome, glad it helped."}]}}`

	chunks := chunkSession(content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	all := chunks[0].Text + "\n" + chunks[1].Text
	if strings.Contains(all, "inspect the tokenizer") {
		t.Error("thinking block text should not appear in chunks")
	}
	if strings.Contains(all, "read_file") || strings.Contains(all, "parser.go") {
		t.Error("toolCall content should not appear in chunks")
	}
	if strings.Contains(all, "raw file contents from the tool") {
		t.Error("toolResult message content should not appear in chunks")
	}
	if !strings.Contains(chunks[0].Text, "off-by-one in the loop") {
		t.Errorf("missing assistant text block: %q", chunks[0].Text)
	}
}

func TestChunkSession_PiIgnoresNonMessageLines(t *testing.T) {
	content := piHeader + `
{"type":"model_change","id":"x1","model":"some-model"}
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"Does the header line get indexed as a turn?"}]}}
{"type":"thinking_level_change","id":"x2","level":"high"}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"No, only message lines with user or assistant roles become turns."}]}}
{"type":"custom","id":"x3","payload":{"anything":"here"}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if strings.Contains(chunks[0].Text, "some-model") || strings.Contains(chunks[0].Text, "payload") {
		t.Error("non-message top-level lines should not appear in chunks")
	}
}

func TestChunkSession_PiNoHeaderID(t *testing.T) {
	// A pi file detected purely by type:"message" lines, with no session id.
	content := `{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"Can pi sessions be detected without a header?"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"Yes, message lines with a nested role are enough to detect pi."}]}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Symbol != "#1" {
		t.Errorf("Symbol = %q, want %q (empty slug)", chunks[0].Symbol, "#1")
	}
}

func TestChunkSession_StrayMessageLineNotPi(t *testing.T) {
	// A non-session .jsonl that happens to contain a single message/role-shaped
	// record should not be misclassified as a pi session. Without a header and
	// without a majority of pi message lines, detection must return "" so File()
	// falls back to generic chunking instead of dropping the file.
	content := `{"name":"Alice","age":30}
{"name":"Bob","age":25}
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"stray record"}]}}
{"name":"Charlie","age":35}
{"name":"Dave","age":40}`

	chunks := chunkSession(content)
	if chunks != nil {
		t.Fatalf("expected nil (not a session), got %d chunks", len(chunks))
	}
}

func TestChunkSession_PiLargeTurnSplit(t *testing.T) {
	paragraphs := make([]string, 0, 20)
	for range 20 {
		paragraphs = append(paragraphs, strings.Repeat("This is paragraph content that fills space. ", 10))
	}
	largeResponse := strings.Join(paragraphs, "\n\n")
	encoded, _ := json.Marshal(largeResponse)
	inner := string(encoded[1 : len(encoded)-1])

	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"Give me a detailed explanation of pi session indexing."}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"` + inner + `"}]}}`

	chunks := chunkSession(content)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large turn, got %d", len(chunks))
	}

	seen := map[int]bool{}
	for i, c := range chunks {
		if c.Language != "session" || c.Kind != "turn" {
			t.Errorf("chunks[%d] = (%q,%q), want (session,turn)", i, c.Language, c.Kind)
		}
		if c.Symbol != "2df495f5#1" {
			t.Errorf("chunks[%d].Symbol = %q, want %q", i, c.Symbol, "2df495f5#1")
		}
		if seen[c.StartLine] {
			t.Errorf("chunks[%d].StartLine = %d is duplicate (FTS dedupe collision)", i, c.StartLine)
		}
		seen[c.StartLine] = true
	}
}

func TestChunkSession_PiContentAlwaysBlockArray(t *testing.T) {
	// pi content is always an array of blocks (never a bare string).
	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"first part of the question "},{"type":"text","text":"and the second part too"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"Both text blocks are concatenated into the user turn."}]}}`

	chunks := chunkSession(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "first part of the question") || !strings.Contains(chunks[0].Text, "and the second part too") {
		t.Errorf("expected both user text blocks: %q", chunks[0].Text)
	}
}

func TestChunkSession_PiViaFileDispatch(t *testing.T) {
	content := piHeader + `
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"Does File() route pi sessions correctly?"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"Yes, .jsonl dispatches to chunkSession which now handles pi."}]}}`

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
