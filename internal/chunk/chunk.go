package chunk

import (
	"path/filepath"
	"strings"
)

type Chunk struct {
	Text      string
	StartLine int
	EndLine   int
	Language  string
	Kind      string // e.g. "function_declaration", "section", "window"
	Symbol    string // e.g. function name, heading path
}

func File(path, content string) ([]Chunk, error) {
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
