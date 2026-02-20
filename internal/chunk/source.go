package chunk

import "strings"

const (
	windowSize = 30
	overlap    = 5
	minChars   = 20
)

func chunkSource(content string) []Chunk {
	lines := strings.Split(content, "\n")
	var chunks []Chunk

	for start := 0; start < len(lines); start += windowSize - overlap {
		end := start + windowSize
		if end > len(lines) {
			end = len(lines)
		}

		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if len(text) < minChars {
			continue
		}

		chunks = append(chunks, Chunk{
			Text:    text,
			LineNum: start + 1,
		})

		if end == len(lines) {
			break
		}
	}

	return chunks
}
