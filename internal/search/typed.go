package search

import "strings"

type MetadataFilters struct {
	Languages []string
	Kinds     []string
	Symbols   []string
}

func (f MetadataFilters) Empty() bool {
	return len(f.Languages) == 0 && len(f.Kinds) == 0 && len(f.Symbols) == 0
}

type SubQuery struct {
	Text    string
	Mode    string // "fts", "vec", or "hybrid"
	Filters MetadataFilters
}

// parseTypedQuery parses a query string with optional lex:/vec: prefixes and | composition.
// Plain queries without prefixes return a single hybrid sub-query.
// Examples:
//
//	"hello"                    → [{Text:"hello", Mode:"hybrid"}]
//	"lex:hello"                → [{Text:"hello", Mode:"fts"}]
//	"vec:semantic meaning"     → [{Text:"semantic meaning", Mode:"vec"}]
//	"lex:exact | vec:similar"  → [{Text:"exact", Mode:"fts"}, {Text:"similar", Mode:"vec"}]
//	"language:go symbol:Open"  → [{Text:"", Mode:"hybrid", Filters:{...}}]
func parseTypedQuery(raw string) []SubQuery {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	hasPipe := strings.Contains(raw, " | ")
	parts := []string{raw}
	if hasPipe {
		parts = nil
		for part := range strings.SplitSeq(raw, " | ") {
			parts = append(parts, part)
		}
	}

	var queries []SubQuery
	for _, rawPart := range parts {
		part := rawPart
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		sq := SubQuery{Mode: "hybrid"}
		if strings.HasPrefix(part, "lex:") {
			sq.Mode = "fts"
			part = strings.TrimSpace(part[4:])
		} else if strings.HasPrefix(part, "vec:") {
			sq.Mode = "vec"
			part = strings.TrimSpace(part[4:])
		}
		sq.Text, sq.Filters = parseMetadataFilters(part)

		if sq.Text != "" || !sq.Filters.Empty() {
			queries = append(queries, sq)
		}
	}

	if len(queries) == 0 {
		return []SubQuery{{Text: raw, Mode: "hybrid"}}
	}
	return queries
}

func parseMetadataFilters(raw string) (string, MetadataFilters) {
	var filters MetadataFilters
	if strings.TrimSpace(raw) == "" {
		return "", filters
	}

	languageSet := map[string]bool{}
	kindSet := map[string]bool{}
	symbolSet := map[string]bool{}
	var textParts []string

	for token := range strings.FieldsSeq(raw) {
		key, value, ok := splitFilterToken(token)
		if !ok {
			textParts = append(textParts, token)
			continue
		}

		switch key {
		case "lang", "language":
			value = strings.ToLower(value)
			if value != "" && !languageSet[value] {
				languageSet[value] = true
				filters.Languages = append(filters.Languages, value)
			}
		case "kind", "chunk_kind":
			value = strings.ToLower(value)
			if value != "" && !kindSet[value] {
				kindSet[value] = true
				filters.Kinds = append(filters.Kinds, value)
			}
		case "sym", "symbol":
			if value != "" {
				value = strings.ToLower(value)
			}
			if value != "" && !symbolSet[value] {
				symbolSet[value] = true
				filters.Symbols = append(filters.Symbols, value)
			}
		default:
			textParts = append(textParts, token)
		}
	}

	return strings.TrimSpace(strings.Join(textParts, " ")), filters
}

func splitFilterToken(token string) (key, value string, ok bool) {
	i := strings.IndexByte(token, ':')
	if i <= 0 || i == len(token)-1 {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(token[:i]))
	value = strings.TrimSpace(token[i+1:])
	value = strings.Trim(value, "\"'")
	if value == "" {
		return "", "", false
	}
	return key, value, true
}
