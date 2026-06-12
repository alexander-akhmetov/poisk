package search

import (
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/store"
)

func appendInClause(query string, args []any, column string, values []string) (string, []any) {
	if len(values) == 0 {
		return query, args
	}
	placeholders := strings.Repeat("?,", len(values))
	placeholders = placeholders[:len(placeholders)-1]
	query += " AND " + column + " IN (" + placeholders + ")"
	for _, v := range values {
		args = append(args, v)
	}
	return query, args
}

func searchVec(s *store.Store, queryBlob []byte, topK int, folders []string, filters MetadataFilters, threshold float64) ([]Result, error) {
	if !s.VecAvailable() {
		return nil, nil
	}

	filtered := !filters.Empty()
	fetchLimit := topK * 5
	if filtered {
		fetchLimit = topK * 10
	}

	results, err := execVecQuery(s, queryBlob, topK, fetchLimit, folders, filters, threshold)
	if err != nil {
		return nil, err
	}

	if filtered && len(results) < topK {
		retryLimit := min(topK*50, 1000)
		results, err = execVecQuery(s, queryBlob, topK, retryLimit, folders, filters, threshold)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func execVecQuery(s *store.Store, queryBlob []byte, topK, fetchLimit int, folders []string, filters MetadataFilters, threshold float64) ([]Result, error) {
	query := `SELECT e.file_path, e.line_num, e.end_line, e.chunk_text, v.distance, e.folder, e.language, e.chunk_kind, e.symbol
		FROM vec_embeddings v
		JOIN embeddings e ON e.id = v.rowid
		WHERE v.embedding MATCH ` + s.VecValueExpr() + ` AND k = ?`
	args := []any{queryBlob, fetchLimit}

	query, args = appendInClause(query, args, "e.source", folders)
	query, args = appendInClause(query, args, "e.language", filters.Languages)
	query, args = appendInClause(query, args, "e.chunk_kind", filters.Kinds)
	query, args = appendInClause(query, args, "LOWER(e.symbol)", filters.Symbols)
	query += " ORDER BY v.distance ASC"

	rows, err := s.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var distance float64
		if err := rows.Scan(&r.FilePath, &r.LineNum, &r.EndLine, &r.Text, &distance, &r.Folder, &r.Language, &r.Kind, &r.Symbol); err != nil {
			return nil, err
		}
		r.Score = 1.0 - distance
		if r.Score >= threshold {
			results = append(results, r)
		}
		if len(results) >= topK {
			break
		}
	}
	return results, rows.Err()
}
