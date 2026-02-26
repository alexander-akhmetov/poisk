package chunk

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxSessionChunkChars = 3000

type sessionLine struct {
	Type    string         `json:"type"`
	Slug    string         `json:"slug"`
	Message sessionMessage `json:"message"`
}

type sessionMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type sessionContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type parsedLine struct {
	lineNum int // 1-based
	data    sessionLine
}

// chunkSession parses Claude Code session JSONL into conversation turn chunks.
// Returns nil if the content is not a recognized session format.
func chunkSession(content string) []Chunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	rawLines := strings.Split(content, "\n")

	var parsed []parsedLine
	for i, raw := range rawLines {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var sl sessionLine
		if err := json.Unmarshal([]byte(raw), &sl); err != nil {
			continue
		}
		parsed = append(parsed, parsedLine{lineNum: i + 1, data: sl})
	}

	if !isSessionFormat(parsed) {
		return nil
	}

	slug := extractSlug(parsed)

	type turn struct {
		userText      string
		assistantText string
		startLine     int
		endLine       int
	}

	var turns []turn
	var current *turn

	for _, pl := range parsed {
		switch pl.data.Type {
		case "user":
			text := extractText(pl.data.Message.Content)
			if text == "" {
				continue
			}
			// Each user message starts a new turn.
			if current != nil {
				turns = append(turns, *current)
			}
			current = &turn{
				userText:  text,
				startLine: pl.lineNum,
				endLine:   pl.lineNum,
			}
		case "assistant":
			text := extractText(pl.data.Message.Content)
			if text == "" {
				continue
			}
			if current == nil {
				// Pre-first-user assistant text: drop it.
				continue
			}
			if current.assistantText != "" {
				current.assistantText += "\n\n" + text
			} else {
				current.assistantText = text
			}
			current.endLine = pl.lineNum
		}
	}
	if current != nil {
		turns = append(turns, *current)
	}

	var chunks []Chunk
	turnNum := 0
	for _, t := range turns {
		turnNum++
		text := formatTurn(t.userText, t.assistantText)
		if len(text) < minChars {
			continue
		}

		symbol := formatSymbol(slug, turnNum)

		if len(text) <= maxSessionChunkChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: t.startLine,
				EndLine:   t.endLine,
				Language:  "session",
				Kind:      "turn",
				Symbol:    symbol,
			})
		} else {
			chunks = append(chunks, splitTurn(text, t.startLine, t.endLine, symbol)...)
		}
	}

	return chunks
}

func isSessionFormat(parsed []parsedLine) bool {
	knownTypes := map[string]bool{
		"user": true, "assistant": true, "system": true, "progress": true,
	}
	checked := 0
	matched := 0
	for _, pl := range parsed {
		if pl.data.Type == "" {
			continue
		}
		checked++
		if knownTypes[pl.data.Type] {
			matched++
		}
		if checked >= 5 {
			break
		}
	}
	return checked > 0 && matched > 0 && matched*2 >= checked
}

func extractSlug(parsed []parsedLine) string {
	for _, pl := range parsed {
		if pl.data.Slug != "" {
			return pl.data.Slug
		}
	}
	return ""
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}

	// Try array of content blocks.
	if raw[0] == '[' {
		var blocks []sessionContentBlock
		if err := json.Unmarshal(raw, &blocks); err == nil {
			var texts []string
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					texts = append(texts, strings.TrimSpace(b.Text))
				}
			}
			return strings.Join(texts, "\n\n")
		}
	}

	return ""
}

func formatTurn(userText, assistantText string) string {
	if assistantText == "" {
		return fmt.Sprintf("User: %s", userText)
	}
	return fmt.Sprintf("User: %s\n\nAssistant: %s", userText, assistantText)
}

func formatSymbol(slug string, turnNum int) string {
	if slug != "" {
		return fmt.Sprintf("%s#%d", slug, turnNum)
	}
	return fmt.Sprintf("#%d", turnNum)
}

func splitTurn(text string, startLine, endLine int, symbol string) []Chunk {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []Chunk

	var buf strings.Builder
	subIdx := 0

	flush := func() {
		if buf.Len() > 0 {
			chunks = append(chunks, makeSubChunk(buf.String(), startLine, endLine, symbol, subIdx))
			buf.Reset()
			subIdx++
		}
	}

	for _, para := range paragraphs {
		if buf.Len() > 0 && buf.Len()+len(para)+2 > maxSessionChunkChars {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(para)

		// Hard-split if a single paragraph exceeds the limit.
		for buf.Len() > maxSessionChunkChars {
			s := buf.String()
			cut := maxSessionChunkChars
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}
			chunks = append(chunks, makeSubChunk(s[:cut], startLine, endLine, symbol, subIdx))
			subIdx++
			buf.Reset()
			buf.WriteString(s[cut:])
		}
	}

	flush()
	return chunks
}

func makeSubChunk(text string, startLine, endLine int, symbol string, subIdx int) Chunk {
	// Offset StartLine by subIdx to avoid FTS dedupe collisions.
	// Ensure EndLine >= StartLine to maintain the invariant.
	line := startLine + subIdx
	end := max(endLine, line)
	return Chunk{
		Text:      text,
		StartLine: line,
		EndLine:   end,
		Language:  "session",
		Kind:      "turn",
		Symbol:    symbol,
	}
}
