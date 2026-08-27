package chunk

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DefaultMaxInputBytes is the byte ceiling every chunk is held to when the
// caller names no other one, and the size the format-specific chunkers aim for.
const DefaultMaxInputBytes = 8000

// minInputBytes is the smallest ceiling the splitter can honour: below four
// bytes it could not emit a single UTF-8 rune.
const minInputBytes = 4

type Chunk struct {
	Text      string
	StartLine int
	EndLine   int
	Language  string
	Kind      string // e.g. "function_declaration", "section", "window"
	Symbol    string // e.g. function name, heading path
}

func File(path, content string) ([]Chunk, error) {
	return FileWithLimit(path, content, DefaultMaxInputBytes)
}

// FileWithLimit chunks content by format, then holds every chunk to maxBytes.
// The format-specific chunkers aim for a size but none of them guarantees one:
// a single Markdown paragraph, source line or syntax node can be arbitrarily
// long, and one such chunk can occupy the embedding provider for minutes.
func FileWithLimit(path, content string, maxBytes int) ([]Chunk, error) {
	chunks, err := chunkByFormat(path, content)
	if err != nil {
		return nil, err
	}
	return capChunkSize(chunks, maxBytes), nil
}

func chunkByFormat(path, content string) ([]Chunk, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return chunkMarkdown(content), nil
	case ".jsonl":
		if chunks := chunkSession(content); chunks != nil {
			return chunks, nil
		}
		return chunkSource(content), nil
	default:
		// Try tree-sitter for supported languages
		if _, ok := extToLang[ext]; ok {
			return chunkTreeSitter(ext, content)
		}
		// Fall back to heuristic source chunking for unknown extensions.
		return chunkSource(content), nil
	}
}

// capChunkSize replaces every chunk longer than maxBytes with the fragments it
// splits into. Fragments keep the original chunk's metadata and line range:
// they describe the same source region, and search tells them apart by their
// stored row id, not by file and line.
func capChunkSize(chunks []Chunk, maxBytes int) []Chunk {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxInputBytes
	}
	maxBytes = max(maxBytes, minInputBytes)

	oversized := false
	for _, c := range chunks {
		if len(c.Text) > maxBytes {
			oversized = true
			break
		}
	}
	if !oversized {
		return chunks
	}

	out := make([]Chunk, 0, len(chunks)+1)
	for _, c := range chunks {
		if len(c.Text) <= maxBytes {
			out = append(out, c)
			continue
		}
		for _, text := range splitToLimit(c.Text, maxBytes) {
			fragment := c
			fragment.Text = text
			out = append(out, fragment)
		}
	}
	return out
}

// splitToLimit cuts text into pieces of at most maxBytes. Concatenating them
// reproduces text byte for byte: nothing is trimmed, added or reordered.
func splitToLimit(text string, maxBytes int) []string {
	var fragments []string
	for len(text) > maxBytes {
		cut := cutPoint(text, maxBytes)
		fragments = append(fragments, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		fragments = append(fragments, text)
	}
	return fragments
}

// cutPoint returns where to cut a string that is longer than maxBytes. It
// prefers the last line break, then the last space, so a fragment ends on a
// boundary a reader would recognise, and never cuts inside a UTF-8 rune.
//
// The search for a boundary stops a quarter of the way back so that text with
// one early newline and nothing after it does not produce a stream of tiny
// fragments. With no boundary in that window it cuts at the rune boundary
// closest to the limit, which is at least maxBytes-3 bytes in, so every cut
// makes progress.
func cutPoint(text string, maxBytes int) int {
	// A rune is at most utf8.UTFMax bytes, so valid UTF-8 has a rune start
	// within that distance of the limit. Text without one there is not valid
	// UTF-8 at all: cut at the limit, because walking further back finds no
	// boundary and would cut such a file one byte at a time.
	limit := maxBytes
	floor := max(maxBytes-utf8.UTFMax+1, 1)
	for limit > floor && !utf8.RuneStart(text[limit]) {
		limit--
	}
	if !utf8.RuneStart(text[limit]) {
		return maxBytes
	}

	earliest := limit - limit/4
	if i := strings.LastIndexByte(text[:limit], '\n'); i >= earliest {
		return i + 1
	}
	if i := strings.LastIndexByte(text[:limit], ' '); i >= earliest {
		return i + 1
	}
	return limit
}
