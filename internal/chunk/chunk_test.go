package chunk

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestChunkSourceStructuredFallback(t *testing.T) {
	lines := make([]string, 120)
	for i := range lines {
		lines[i] = "line with enough body text for semantic chunking " + strings.Repeat("x", 30)
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
	if chunks[0].Kind == "" {
		t.Fatal("chunk kind should be set")
	}
}

func TestChunkSourceCommentBoundaryHeuristic(t *testing.T) {
	content := strings.Join([]string{
		"// This comment block explains fallback chunking behavior in enough detail to avoid tiny-block merging.",
		"// It should remain a coherent comment chunk and not be merged into the following function signature.",
		"func runTask(input string) string {",
		"\treturn input + \"-ok\"",
		"}",
	}, "\n")

	lines := strings.Split(content, "\n")
	blocks := detectSourceBlocks(lines, buildLinePrefix(lines))
	if len(blocks) < 2 {
		t.Fatalf("got %d blocks, want >= 2", len(blocks))
	}

	firstBlockText := strings.TrimSpace(strings.Join(lines[blocks[0].start:blocks[0].end+1], "\n"))
	if !strings.HasPrefix(firstBlockText, "// This comment block") {
		t.Fatalf("unexpected first block text: %q", firstBlockText)
	}
	if strings.Contains(firstBlockText, "func runTask") {
		t.Fatalf("comment block should be split from function signature, got %q", firstBlockText)
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

func TestChunkSourceLongSpanSplit(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("function processData(input) {\n")
	for range 500 {
		sb.WriteString("  const line = input + \"-")
		sb.WriteString(strings.Repeat("x", 25))
		sb.WriteString("\";\n")
	}
	sb.WriteString("}\n")

	chunks, err := File("big.txt", sb.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2 for long source span", len(chunks))
	}
	for _, c := range chunks {
		if len(c.Text) > fallbackMaxChars*2 {
			t.Errorf("chunk too large: %d chars", len(c.Text))
		}
	}
}

func TestChunkSourceTinyNoisyFile(t *testing.T) {
	content := ";\n#\n//\n{}\n"
	chunks, err := File("noise.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("got %d chunks, want 0 for tiny noisy file", len(chunks))
	}
}

func TestChunkSourceOverlapMetadata(t *testing.T) {
	var lines []string
	for i := range 140 {
		if i%8 == 0 {
			lines = append(lines, "Section: "+strings.Repeat("S", 40))
			continue
		}
		lines = append(lines, "body line "+strings.Repeat("x", 50))
	}
	content := strings.Join(lines, "\n")

	chunks, err := File("meta.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	totalLines := len(strings.Split(content, "\n"))
	hasOverlap := false
	for i, c := range chunks {
		if c.StartLine < 1 || c.EndLine > totalLines || c.EndLine < c.StartLine {
			t.Fatalf("invalid metadata for chunk %d: start=%d end=%d total=%d", i, c.StartLine, c.EndLine, totalLines)
		}
		gotTextLines := strings.Count(c.Text, "\n") + 1
		wantTextLines := c.EndLine - c.StartLine + 1
		if gotTextLines != wantTextLines {
			t.Fatalf("chunk %d line mismatch: text has %d lines, metadata says %d", i, gotTextLines, wantTextLines)
		}
		if i == 0 {
			continue
		}

		prev := chunks[i-1]
		if c.StartLine <= prev.EndLine {
			hasOverlap = true
		}
		if c.StartLine <= prev.StartLine {
			t.Fatalf("chunk %d start line %d should be > previous start line %d", i, c.StartLine, prev.StartLine)
		}
		if c.StartLine > prev.EndLine+1 {
			t.Fatalf("chunk %d has gap: start=%d previous end=%d", i, c.StartLine, prev.EndLine)
		}
	}
	if !hasOverlap {
		t.Fatal("expected at least one overlapping chunk boundary")
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
	// Create a section that exceeds maxSectionBytes
	var sb strings.Builder
	sb.WriteString("# Big Section\n\n")
	for range 200 {
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
		if len(c.Text) > maxSectionBytes*2 {
			t.Errorf("chunk too large: %d chars", len(c.Text))
		}
	}
}

// oneLine returns a single line of body bytes long, with no newline and no
// place for a chunker to split semantically.
func oneLine(body int) string {
	return strings.Repeat("x", body)
}

func TestFileWithLimitCapsEveryDispatchPath(t *testing.T) {
	const oversized = 100 * 1024

	tests := []struct {
		name    string
		path    string
		content string
	}{
		{
			name:    "markdown paragraph",
			path:    "big.md",
			content: "# Heading\n\n" + oneLine(oversized),
		},
		{
			name:    "unknown extension falls back to source chunking",
			path:    "big.txt",
			content: oneLine(oversized),
		},
		{
			name:    "tree-sitter go node",
			path:    "big.go",
			content: "package main\n\nvar Blob = \"" + oneLine(oversized) + "\"\n",
		},
		{
			name:    "jsonl that is not a session",
			path:    "big.jsonl",
			content: "{\"blob\":\"" + oneLine(oversized) + "\"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, limit := range []int{4, 100, 8000} {
				chunks, err := FileWithLimit(tt.path, tt.content, limit)
				if err != nil {
					t.Fatalf("limit %d: %v", limit, err)
				}
				if len(chunks) == 0 {
					t.Fatalf("limit %d: no chunks", limit)
				}
				for i, c := range chunks {
					if len(c.Text) > limit {
						t.Fatalf("limit %d: chunk %d is %d bytes", limit, i, len(c.Text))
					}
					if !utf8.ValidString(c.Text) {
						t.Fatalf("limit %d: chunk %d is not valid UTF-8", limit, i)
					}
				}
			}
		})
	}
}

func TestFileWithLimitDoesNotShredContentThatIsNotUTF8(t *testing.T) {
	const limit = 8000
	// A .txt holding UTF-16 or binary reaches the chunkers like any other
	// document, and it has no rune start to cut on.
	content := strings.Repeat("\x80", 60*1024)

	chunks, err := FileWithLimit("notes.txt", content, limit)
	if err != nil {
		t.Fatal(err)
	}

	want := (len(content) + limit - 1) / limit
	if len(chunks) > want+1 {
		t.Fatalf("got %d chunks for %d bytes, want about %d", len(chunks), len(content), want)
	}
	for i, c := range chunks {
		if len(c.Text) > limit {
			t.Fatalf("chunk %d is %d bytes", i, len(c.Text))
		}
	}
}

func TestCapChunkSizePreservesTextAndMetadata(t *testing.T) {
	original := Chunk{
		Text:      strings.Repeat("héllo wörld ", 2000),
		StartLine: 12,
		EndLine:   40,
		Language:  "markdown",
		Kind:      "paragraph",
		Symbol:    "# Heading",
	}

	tests := []struct {
		name    string
		maxByte int
		want    int // fragments expected, 0 means "more than one"
	}{
		{name: "one byte under the limit", maxByte: len(original.Text) + 1, want: 1},
		{name: "exactly the limit", maxByte: len(original.Text), want: 1},
		{name: "one byte over the limit", maxByte: len(original.Text) - 1, want: 2},
		{name: "small limit", maxByte: 64, want: 0},
		{name: "smallest limit that holds a rune", maxByte: 4, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capChunkSize([]Chunk{original}, tt.maxByte)
			if tt.want > 0 && len(got) != tt.want {
				t.Fatalf("got %d fragments, want %d", len(got), tt.want)
			}
			if tt.want == 0 && len(got) < 2 {
				t.Fatalf("got %d fragments, want more than one", len(got))
			}

			var rebuilt strings.Builder
			for i, c := range got {
				if len(c.Text) > tt.maxByte {
					t.Fatalf("fragment %d is %d bytes, over the %d limit", i, len(c.Text), tt.maxByte)
				}
				if !utf8.ValidString(c.Text) {
					t.Fatalf("fragment %d cuts a rune", i)
				}
				if c.StartLine != original.StartLine || c.EndLine != original.EndLine ||
					c.Language != original.Language || c.Kind != original.Kind || c.Symbol != original.Symbol {
					t.Fatalf("fragment %d changed metadata: %+v", i, c)
				}
				rebuilt.WriteString(c.Text)
			}
			if rebuilt.String() != original.Text {
				t.Fatal("fragments do not reassemble into the original text")
			}
		})
	}
}

func TestSplitToLimitBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxBytes int
		want     []string
	}{
		{
			name:     "text at the limit is one fragment",
			text:     strings.Repeat("a", 8),
			maxBytes: 8,
			want:     []string{strings.Repeat("a", 8)},
		},
		{
			name:     "one byte over leaves a tiny tail",
			text:     strings.Repeat("a", 9),
			maxBytes: 8,
			want:     []string{strings.Repeat("a", 8), "a"},
		},
		{
			name:     "cuts after a newline in the boundary window",
			text:     "aaaaaaaaaaaa\nbbbbbb",
			maxBytes: 16,
			want:     []string{"aaaaaaaaaaaa\n", "bbbbbb"},
		},
		{
			name:     "cuts after a space when there is no newline",
			text:     "aaaaaaaaaaaa bbbbbb",
			maxBytes: 16,
			want:     []string{"aaaaaaaaaaaa ", "bbbbbb"},
		},
		{
			name:     "ignores a boundary too far back to make progress",
			text:     "a bcdefghijklmnop",
			maxBytes: 8,
			want:     []string{"a bcdefg", "hijklmno", "p"},
		},
		{
			name:     "backs off a four-byte rune crossing the cut",
			text:     "aaaaaa\U0001F600b",
			maxBytes: 8,
			want:     []string{"aaaaaa", "\U0001F600b"},
		},
		{
			// Text with no rune start anywhere: a .txt holding UTF-16 or
			// binary. Searching further back for one would cut it a byte at a
			// time and turn one file into tens of thousands of chunks.
			name:     "text that is not UTF-8 cuts at the limit",
			text:     strings.Repeat("\x80", 20),
			maxBytes: 8,
			want:     []string{strings.Repeat("\x80", 8), strings.Repeat("\x80", 8), strings.Repeat("\x80", 4)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitToLimit(tt.text, tt.maxBytes)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("splitToLimit(%q, %d) = %q, want %q", tt.text, tt.maxBytes, got, tt.want)
			}
		})
	}
}
