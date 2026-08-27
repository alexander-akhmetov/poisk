package chunk

import "strings"

// maxSectionBytes is what the Markdown splitter aims for. capChunkSize holds
// the result to the configured ceiling; this only decides where sections split.
const maxSectionBytes = DefaultMaxInputBytes

func chunkMarkdown(content string) []Chunk {
	lines := strings.Split(content, "\n")
	var chunks []Chunk

	// Heading stack: tracks the current heading path by level
	headingStack := make([]string, 7) // indices 1-6 for h1-h6
	var currentHeadingPath string

	var para []string
	paraStart := 1
	inFence := false

	flush := func() {
		text := strings.TrimSpace(strings.Join(para, "\n"))
		if len(text) < 20 {
			para = nil
			return
		}
		if currentHeadingPath != "" {
			text = currentHeadingPath + "\n\n" + text
		}
		endLine := paraStart + len(para) - 1

		// Token-budget splitting for large sections
		if len(text) > maxSectionBytes {
			subChunks := splitLargeSection(text, paraStart, endLine, currentHeadingPath)
			chunks = append(chunks, subChunks...)
		} else {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: paraStart,
				EndLine:   endLine,
				Language:  "markdown",
				Kind:      "section",
				Symbol:    currentHeadingPath,
			})
		}
		para = nil
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Fence detection
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			para = append(para, line)
			continue
		}

		// Inside a fenced block: accumulate without splitting
		if inFence {
			para = append(para, line)
			continue
		}

		// ATX Heading: # followed by space or end-of-line
		if isATXHeading(trimmed) {
			flush()
			level := headingLevel(trimmed)
			if level > 0 && level <= 6 {
				headingStack[level] = trimmed
				// Clear deeper levels
				for j := level + 1; j <= 6; j++ {
					headingStack[j] = ""
				}
				currentHeadingPath = buildHeadingPath(headingStack)
			}
			paraStart = lineNum
			continue
		}

		// Blank line (paragraph break)
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

func isATXHeading(line string) bool {
	if len(line) == 0 || line[0] != '#' {
		return false
	}
	level := 0
	for _, ch := range line {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	// Valid ATX heading: 1-6 # followed by space or end-of-line
	return level >= 1 && level <= 6 && (len(line) == level || line[level] == ' ')
}

func headingLevel(line string) int {
	level := 0
	for _, ch := range line {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level > 6 {
		level = 6
	}
	return level
}

func buildHeadingPath(stack []string) string {
	var parts []string
	for i := 1; i <= 6; i++ {
		if stack[i] != "" {
			parts = append(parts, stack[i])
		}
	}
	return strings.Join(parts, " > ")
}

func splitLargeSection(text string, startLine, endLine int, symbol string) []Chunk {
	// Split by paragraphs (double newline)
	paragraphs := strings.Split(text, "\n\n")
	var chunks []Chunk
	var buf strings.Builder
	bufStart := startLine
	lineOffset := startLine

	for _, para := range paragraphs {
		paraLines := strings.Count(para, "\n") + 1

		if buf.Len()+len(para) > maxSectionBytes && buf.Len() > 0 {
			t := strings.TrimSpace(buf.String())
			if len(t) >= 20 {
				chunks = append(chunks, Chunk{
					Text:      t,
					StartLine: bufStart,
					EndLine:   lineOffset - 1,
					Language:  "markdown",
					Kind:      "paragraph",
					Symbol:    symbol,
				})
			}
			buf.Reset()
			bufStart = lineOffset
		}

		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(para)
		lineOffset += paraLines + 1 // +1 for the blank line between paragraphs
	}

	if buf.Len() > 0 {
		t := strings.TrimSpace(buf.String())
		if len(t) >= 20 {
			chunks = append(chunks, Chunk{
				Text:      t,
				StartLine: bufStart,
				EndLine:   endLine,
				Language:  "markdown",
				Kind:      "paragraph",
				Symbol:    symbol,
			})
		}
	}

	return chunks
}
