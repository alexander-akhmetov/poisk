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
	ID      string         `json:"id"` // pi: session id (header line)
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

type turn struct {
	userText      string
	assistantText string
	startLine     int
	endLine       int
}

// chunkSession parses coding-agent session JSONL into conversation turn chunks.
// It understands two schemas: Claude Code (top-level type:"user"/"assistant",
// top-level slug) and pi (top-level type:"message" with nested message.role,
// a type:"session" header carrying the session id). Returns nil if the content
// is not a recognized session format.
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

	format := detectSessionFormat(parsed)
	if format == "" {
		return nil
	}

	slug := sessionSlug(parsed, format)
	turns := buildTurns(parsed, format)

	return emitTurnChunks(turns, slug)
}

// buildTurns groups user/assistant text into turns. Each user message starts a
// new turn; assistant text is appended to the current turn. Roles other than
// user/assistant (system, progress, pi toolResult) and non-text blocks
// (thinking, toolCall) are dropped by extractText / effectiveRole.
func buildTurns(parsed []parsedLine, format string) []turn {
	var turns []turn
	var current *turn

	for _, pl := range parsed {
		switch effectiveRole(pl, format) {
		case "user":
			text := extractText(pl.data.Message.Content)
			if text == "" {
				continue
			}
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
	return turns
}

func emitTurnChunks(turns []turn, slug string) []Chunk {
	chunks := []Chunk{}
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

// effectiveRole returns the conversation role for a line, normalizing across
// formats. For Claude Code the role is the top-level type; for pi only
// type:"message" lines carry a role, nested under message.role.
func effectiveRole(pl parsedLine, format string) string {
	if format == "pi" {
		if pl.data.Type != "message" {
			return ""
		}
		return pl.data.Message.Role
	}
	return pl.data.Type
}

// detectSessionFormat returns "claude", "pi", or "" (not a session) based on
// the first few parsed lines.
func detectSessionFormat(parsed []parsedLine) string {
	claudeTypes := map[string]bool{
		"user": true, "assistant": true, "system": true, "progress": true,
	}
	piHeader := false
	piMessages := 0
	claudeMatched := 0
	inspected := 0
	for _, pl := range parsed {
		inspected++
		t := pl.data.Type
		switch {
		case t == "session":
			piHeader = true
		case t == "message" && (pl.data.Message.Role == "user" || pl.data.Message.Role == "assistant"):
			piMessages++
		case claudeTypes[t]:
			claudeMatched++
		}
		if inspected >= 5 {
			break
		}
	}
	// A type:"session" header is an unambiguous pi marker. Without it, require a
	// majority of the inspected lines to be pi message lines so a stray
	// message/role-shaped record in an unrelated .jsonl file doesn't get
	// misclassified as a session (it should fall through to generic chunking).
	// Only user/assistant roles count: buildTurns drops everything else, so a
	// log full of message/role records like toolResult would otherwise be
	// detected as pi but produce zero turns, yielding a non-nil empty slice that
	// suppresses the generic fallback chunking in File().
	if piHeader {
		return "pi"
	}
	if piMessages > 0 && piMessages*2 >= inspected {
		return "pi"
	}
	if claudeMatched > 0 && claudeMatched*2 >= inspected {
		return "claude"
	}
	return ""
}

// sessionSlug derives the per-session symbol prefix. Claude Code carries a
// top-level slug; pi has no slug, so we use the first 8 chars of the header id.
func sessionSlug(parsed []parsedLine, format string) string {
	if format == "pi" {
		// Use the header (type:"session") id; message lines carry their own
		// per-line ids that must not become the slug.
		for _, pl := range parsed {
			if pl.data.Type == "session" && pl.data.ID != "" {
				return shortID(pl.data.ID)
			}
		}
		return ""
	}
	return extractSlug(parsed)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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
