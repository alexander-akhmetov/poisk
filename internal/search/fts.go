package search

import (
	"math"
	"strings"
	"unicode"

	"github.com/akhmetov/poisk/internal/store"
)

func buildFTSQuery(text string) string {
	var tokens []string
	var current []rune

	for _, ch := range text {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
			current = append(current, ch)
		} else if len(current) > 0 {
			tokens = append(tokens, string(current))
			current = nil
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}

	if len(tokens) == 0 {
		return ""
	}

	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " AND ")
}

func searchFTS(s *store.Store, queryText string, topK int, folder string) ([]Result, error) {
	if !s.FTSAvailable() {
		return nil, nil
	}

	ftsQuery := buildFTSQuery(queryText)
	if ftsQuery == "" {
		return nil, nil
	}

	fetchLimit := topK * 5

	query := `SELECT file_path, line_num, chunk_text, bm25(chunks_fts) AS rank, folder
		FROM chunks_fts
		WHERE chunks_fts MATCH ?`
	args := []any{ftsQuery}

	if folder != "" {
		query += " AND source = ?"
		args = append(args, folder)
	}
	query += " ORDER BY rank ASC LIMIT ?"
	args = append(args, fetchLimit)

	rows, err := s.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var rank float64
		var lineStr string
		if err := rows.Scan(&r.FilePath, &lineStr, &r.Text, &rank, &r.Folder); err != nil {
			return nil, err
		}
		// Parse line number from string (FTS5 stores as text)
		for _, ch := range lineStr {
			if ch >= '0' && ch <= '9' {
				r.LineNum = r.LineNum*10 + int(ch-'0')
			}
		}
		// Normalize BM25: abs(rank)/(1+abs(rank)) → [0,1]
		ar := math.Abs(rank)
		r.Score = ar / (1.0 + ar)
		results = append(results, r)
	}

	if len(results) > topK {
		results = results[:topK]
	}
	return results, rows.Err()
}
