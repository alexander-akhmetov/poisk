package chunk

import "strings"

func chunkMarkdown(content string) []Chunk {
	lines := strings.Split(content, "\n")
	var chunks []Chunk
	var heading string
	var para []string
	paraStart := 1

	flush := func() {
		text := strings.TrimSpace(strings.Join(para, "\n"))
		if len(text) < 20 {
			para = nil
			return
		}
		if heading != "" {
			text = heading + "\n\n" + text
		}
		chunks = append(chunks, Chunk{Text: text, LineNum: paraStart})
		para = nil
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			flush()
			heading = trimmed
			paraStart = lineNum
			continue
		}

		if trimmed == "" {
			if len(para) > 0 {
				flush()
			}
			paraStart = lineNum + 1
			continue
		}

		para = append(para, line)
	}

	flush()
	return chunks
}
