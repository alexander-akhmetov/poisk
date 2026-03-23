package search

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/alexander-akhmetov/poisk/internal/store"
)

// tokenize splits text into code-aware tokens.
// It extracts words from the input, then splits camelCase and snake_case,
// preserving originals alongside the sub-tokens.
func tokenize(text string) []string {
	// Extract raw words (letters, digits, underscores)
	var rawTokens []string
	var current []rune
	for _, ch := range text {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
			current = append(current, ch)
		} else if len(current) > 0 {
			rawTokens = append(rawTokens, string(current))
			current = nil
		}
	}
	if len(current) > 0 {
		rawTokens = append(rawTokens, string(current))
	}

	seen := make(map[string]bool)
	var result []string
	add := func(s string) {
		lower := strings.ToLower(s)
		if lower == "" || seen[lower] {
			return
		}
		seen[lower] = true
		result = append(result, lower)
	}

	for _, tok := range rawTokens {
		// Split snake_case first
		snakeParts := strings.Split(tok, "_")
		if len(snakeParts) > 1 {
			for _, part := range snakeParts {
				if part == "" {
					continue
				}
				// Split camelCase within each snake part
				for _, sub := range splitCamel(part) {
					add(sub)
				}
			}
			add(tok) // preserve original with underscores
		} else {
			// Split camelCase
			camelParts := splitCamel(tok)
			if len(camelParts) > 1 {
				for _, sub := range camelParts {
					add(sub)
				}
				add(tok) // preserve original
			} else {
				add(tok)
			}
		}
	}

	return result
}

// splitCamel splits a camelCase or PascalCase string into parts.
// "getHTTPClient" → ["get", "HTTP", "Client"]
// "simpleWord" → ["simple", "Word"]
func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prevUpper := unicode.IsUpper(runes[i-1])
		currUpper := unicode.IsUpper(runes[i])
		currLower := unicode.IsLower(runes[i])

		// Split at lower→upper boundary: "getH" → split before H
		if !prevUpper && currUpper {
			parts = append(parts, string(runes[start:i]))
			start = i
			continue
		}
		// Split at upper→lower when preceded by uppercase run: "HTTPClient" → "HTTP" + "Client"
		if prevUpper && currLower && i-start > 1 {
			parts = append(parts, string(runes[start:i-1]))
			start = i - 1
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

func buildStrictAND(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " AND ")
}

func buildRelaxedOR(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " OR ")
}

func buildPrefixOR(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = `"` + t + `"` + "*"
	}
	return strings.Join(parts, " OR ")
}

func escapeFTSToken(token string) string {
	return strings.ReplaceAll(token, `"`, `""`)
}

func buildColumnFilterClause(column string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf(`%s:"%s"`, column, escapeFTSToken(v)))
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func buildMetadataClause(filters MetadataFilters) string {
	var clauses []string
	if c := buildColumnFilterClause("language", filters.Languages); c != "" {
		clauses = append(clauses, c)
	}
	if c := buildColumnFilterClause("chunk_kind", filters.Kinds); c != "" {
		clauses = append(clauses, c)
	}
	if c := buildColumnFilterClause("symbol", filters.Symbols); c != "" {
		clauses = append(clauses, c)
	}
	return strings.Join(clauses, " AND ")
}

func combineFTSQuery(textClause, metadataClause string) string {
	if textClause == "" {
		return metadataClause
	}
	if metadataClause == "" {
		return textClause
	}
	return textClause + " AND " + metadataClause
}

// searchFTS performs staged FTS retrieval: strict AND → relaxed OR → prefix OR.
// Each stage only runs if prior stages returned fewer results than topK.
// Results are deduplicated across stages by filepath:line.
func searchFTS(s *store.Store, queryText string, topK int, folders []string, filters MetadataFilters) ([]Result, error) {
	if !s.FTSAvailable() {
		return nil, nil
	}

	tokens := tokenize(queryText)
	if len(tokens) == 0 && filters.Empty() {
		return nil, nil
	}

	seen := make(map[string]bool)
	var results []Result
	metadataClause := buildMetadataClause(filters)

	addResults := func(staged []Result) {
		for _, r := range staged {
			key := resultKey(r)
			if !seen[key] {
				seen[key] = true
				results = append(results, r)
			}
		}
	}

	// Stage A: strict AND
	if q := combineFTSQuery(buildStrictAND(tokens), metadataClause); q != "" {
		rows, err := queryFTS(s, q, topK*5, folders)
		if err != nil {
			return nil, err
		}
		addResults(rows)
	}

	// Stage B: relaxed OR (only if A returned < topK)
	if len(results) < topK && len(tokens) > 0 {
		if q := combineFTSQuery(buildRelaxedOR(tokens), metadataClause); q != "" {
			rows, err := queryFTS(s, q, topK*5, folders)
			if err != nil {
				return nil, err
			}
			addResults(rows)
		}
	}

	// Stage C: prefix OR (only if A+B returned < topK)
	if len(results) < topK && len(tokens) > 0 {
		if q := combineFTSQuery(buildPrefixOR(tokens), metadataClause); q != "" {
			rows, err := queryFTS(s, q, topK*5, folders)
			if err != nil {
				return nil, err
			}
			addResults(rows)
		}
	}

	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func resultKey(r Result) string {
	return fmt.Sprintf("%s:%d", r.FilePath, r.LineNum)
}

func queryFTS(s *store.Store, ftsQuery string, limit int, folders []string) ([]Result, error) {
	query := `SELECT file_path, line_num, end_line, chunk_text, bm25(chunks_fts) AS rank, folder, language, chunk_kind, symbol
		FROM chunks_fts
		WHERE chunks_fts MATCH ?`
	args := make([]any, 0, 2)
	args = append(args, ftsQuery)

	query, args = appendInClause(query, args, "source", folders)
	query += " ORDER BY rank ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var rank float64
		var lineStr, endLineStr string
		var err error
		if err = rows.Scan(&r.FilePath, &lineStr, &endLineStr, &r.Text, &rank, &r.Folder, &r.Language, &r.Kind, &r.Symbol); err != nil {
			return nil, err
		}
		r.LineNum, err = strconv.Atoi(lineStr)
		if err != nil {
			return nil, fmt.Errorf("parsing line_num %q for %s: %w", lineStr, r.FilePath, err)
		}
		r.EndLine, err = strconv.Atoi(endLineStr)
		if err != nil {
			return nil, fmt.Errorf("parsing end_line %q for %s: %w", endLineStr, r.FilePath, err)
		}
		ar := math.Abs(rank)
		r.Score = ar / (1.0 + ar)
		results = append(results, r)
	}

	return results, rows.Err()
}
