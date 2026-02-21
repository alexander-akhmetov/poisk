package search

import "strings"

type SubQuery struct {
	Text string
	Mode string // "fts", "vec", or "hybrid"
}

// parseTypedQuery parses a query string with optional lex:/vec: prefixes and | composition.
// Plain queries without prefixes return a single hybrid sub-query.
// Examples:
//
//	"hello"                    → [{Text:"hello", Mode:"hybrid"}]
//	"lex:hello"                → [{Text:"hello", Mode:"fts"}]
//	"vec:semantic meaning"     → [{Text:"semantic meaning", Mode:"vec"}]
//	"lex:exact | vec:similar"  → [{Text:"exact", Mode:"fts"}, {Text:"similar", Mode:"vec"}]
func parseTypedQuery(raw string) []SubQuery {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// Check if the query uses any typed syntax
	hasTypedPrefix := strings.HasPrefix(raw, "lex:") || strings.HasPrefix(raw, "vec:")
	hasPipe := strings.Contains(raw, " | ")

	if !hasTypedPrefix && !hasPipe {
		return []SubQuery{{Text: raw, Mode: "hybrid"}}
	}

	var queries []SubQuery
	for part := range strings.SplitSeq(raw, " | ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		sq := SubQuery{Mode: "hybrid"}
		if strings.HasPrefix(part, "lex:") {
			sq.Mode = "fts"
			sq.Text = strings.TrimSpace(part[4:])
		} else if strings.HasPrefix(part, "vec:") {
			sq.Mode = "vec"
			sq.Text = strings.TrimSpace(part[4:])
		} else {
			sq.Text = part
		}

		if sq.Text != "" {
			queries = append(queries, sq)
		}
	}

	if len(queries) == 0 {
		return []SubQuery{{Text: raw, Mode: "hybrid"}}
	}
	return queries
}
