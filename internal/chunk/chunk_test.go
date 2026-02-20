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
	chunks := ChunkFile("test.md", content)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	// First chunk should have heading context
	if !strings.Contains(chunks[0].Text, "# Introduction") {
		t.Error("first chunk missing heading")
	}
	if !strings.Contains(chunks[0].Text, "first paragraph") {
		t.Error("first chunk missing paragraph text")
	}
}

func TestChunkMarkdownShortParagraphSkipped(t *testing.T) {
	content := `# Title

Short.

This paragraph has enough text to be included as a chunk in the output.
`
	chunks := ChunkFile("test.md", content)
	for _, c := range chunks {
		if c.Text == "Short." || strings.HasSuffix(c.Text, "\n\nShort.") {
			t.Error("short paragraph should be skipped")
		}
	}
}

func TestChunkSource(t *testing.T) {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "// line " + string(rune('A'+i%26))
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("test.go", content)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	// First chunk starts at line 1
	if chunks[0].LineNum != 1 {
		t.Errorf("first chunk line = %d, want 1", chunks[0].LineNum)
	}
}

func TestChunkSourceShortFile(t *testing.T) {
	content := "package main\n\nfunc main() {}\n"
	chunks := ChunkFile("main.go", content)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
}

func TestChunkSourceTooShort(t *testing.T) {
	content := "// hi\n"
	chunks := ChunkFile("tiny.go", content)
	if len(chunks) != 0 {
		t.Fatalf("got %d chunks, want 0 for tiny file", len(chunks))
	}
}
