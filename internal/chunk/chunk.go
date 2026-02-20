package chunk

import (
	"path/filepath"
	"strings"
)

type Chunk struct {
	Text    string
	LineNum int
}

func ChunkFile(path, content string) []Chunk {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return chunkMarkdown(content)
	default:
		return chunkSource(content)
	}
}
