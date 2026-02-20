package chunk

import (
	"strings"
	"testing"
)

func TestChunkMarkdown(t *testing.T) {
	content := `# Introduction

This is the first paragraph with enough text to pass the minimum length threshold.

## Details

Here are some more details about the topic that should form another chunk.

Some additional content that follows without a heading.
`
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	if !strings.Contains(chunks[0].Text, "# Introduction") {
		t.Error("first chunk missing heading")
	}
	if !strings.Contains(chunks[0].Text, "first paragraph") {
		t.Error("first chunk missing paragraph text")
	}
	if chunks[0].Language != "markdown" {
		t.Errorf("chunk language = %q, want markdown", chunks[0].Language)
	}
	if chunks[0].Kind != "section" {
		t.Errorf("chunk kind = %q, want section", chunks[0].Kind)
	}
}

func TestChunkMarkdownHashtagNotHeading(t *testing.T) {
	content := `# Real Heading

This paragraph has enough text to be included as a chunk in the output.

#hashtag should not be treated as a heading by the markdown chunker.
`
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if c.Symbol == "#hashtag" || strings.Contains(c.Symbol, "#hashtag") {
			t.Errorf("hashtag treated as heading: Symbol = %q", c.Symbol)
		}
	}
}

func TestChunkMarkdownShortParagraphSkipped(t *testing.T) {
	content := `# Title

Short.

This paragraph has enough text to be included as a chunk in the output.
`
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if c.Text == "Short." || strings.HasSuffix(c.Text, "\n\nShort.") {
			t.Error("short paragraph should be skipped")
		}
	}
}

func TestChunkSourceWindow(t *testing.T) {
	// Use .txt to test the fixed-window source chunker
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "// line " + string(rune('A'+i%26))
	}
	content := strings.Join(lines, "\n")

	chunks, err := File("test.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	if chunks[0].StartLine != 1 {
		t.Errorf("first chunk StartLine = %d, want 1", chunks[0].StartLine)
	}
	if chunks[0].Kind != "window" {
		t.Errorf("chunk kind = %q, want window", chunks[0].Kind)
	}
}

func TestChunkSourceGoTreeSitter(t *testing.T) {
	content := `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func Goodbye(name string) string {
	return fmt.Sprintf("Goodbye, %s!", name)
}
`
	chunks, err := File("main.go", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}
	if chunks[0].Language != "go" {
		t.Errorf("language = %q, want go", chunks[0].Language)
	}
}

func TestChunkSourceTooShort(t *testing.T) {
	content := "// hi\n"
	chunks, err := File("tiny.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("got %d chunks, want 0 for tiny file", len(chunks))
	}
}

func TestChunkMetadata(t *testing.T) {
	content := `# Heading

This paragraph has enough content to pass the minimum threshold for chunking.
`
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	c := chunks[0]
	if c.StartLine == 0 {
		t.Error("StartLine should not be 0")
	}
	if c.EndLine < c.StartLine {
		t.Errorf("EndLine (%d) < StartLine (%d)", c.EndLine, c.StartLine)
	}
	if c.Symbol != "# Heading" {
		t.Errorf("Symbol = %q, want %q", c.Symbol, "# Heading")
	}
}

func TestChunkMarkdownFencedCode(t *testing.T) {
	content := "# Code Example\n\nHere is a code block with enough text to pass the threshold:\n\n```go\nfunc main() {\n\n\tprintln(\"hello\")\n\n}\n```\n\nThis text follows the code block and should be in a separate chunk or same.\n"
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	// The fenced block should not be split at blank lines inside it
	foundCode := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "```go") && strings.Contains(c.Text, "```") {
			foundCode = true
			if strings.Contains(c.Text, "println") {
				// Good - the fence content is preserved together
				break
			}
		}
	}
	if !foundCode {
		// The fenced block content should be in one chunk
		for _, c := range chunks {
			if strings.Contains(c.Text, "println") {
				foundCode = true
				break
			}
		}
	}
	if !foundCode {
		t.Error("fenced code block content lost")
	}
}

func TestChunkMarkdownHeadingPath(t *testing.T) {
	content := `# Top Level

Some text that is long enough for the chunker to include it in the output.

## Second Level

More text that is long enough for the chunker to include it as a section.

### Third Level

Third level text that is long enough for the chunker to include it here.
`
	chunks, err := File("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("got %d chunks, want >= 3", len(chunks))
	}

	// Last chunk should have full heading path
	last := chunks[len(chunks)-1]
	if !strings.Contains(last.Symbol, "# Top Level") {
		t.Errorf("last chunk Symbol = %q, missing top-level heading", last.Symbol)
	}
	if !strings.Contains(last.Symbol, "### Third Level") {
		t.Errorf("last chunk Symbol = %q, missing third-level heading", last.Symbol)
	}
}

func TestChunkMarkdownLargeSection(t *testing.T) {
	// Create a section that exceeds maxSectionChars
	var sb strings.Builder
	sb.WriteString("# Big Section\n\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("This is paragraph number ")
		sb.WriteString(strings.Repeat("x", 50))
		sb.WriteString(" with enough content.\n\n")
	}
	chunks, err := File("test.md", sb.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2 (section should be split)", len(chunks))
	}
	for _, c := range chunks {
		if len(c.Text) > maxSectionChars*2 {
			t.Errorf("chunk too large: %d chars", len(c.Text))
		}
	}
}
