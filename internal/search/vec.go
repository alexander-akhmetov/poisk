package search

import (
	"strings"

	"github.com/akhmetov/poisk/internal/store"
)

func searchVec(s *store.Store, queryBlob []byte, topK int, folders []string, threshold float64) ([]Result, error) {
	if !s.VecAvailable() {
		return nil, nil
	}

	fetchLimit := topK * 5

	query := `SELECT e.file_path, e.line_num, e.end_line, e.chunk_text, v.distance, e.folder, e.language, e.chunk_kind, e.symbol
		FROM vec_embeddings v
		JOIN embeddings e ON e.id = v.rowid
		WHERE v.embedding MATCH ? AND k = ?`
	args := []any{queryBlob, fetchLimit}

	if len(folders) > 0 {
		placeholders := strings.Repeat("?,", len(folders))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND e.source IN (" + placeholders + ")"
		for _, f := range folders {
			args = append(args, f)
		}
	}
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
		r.Score = 1.0 - distance // cosine distance → similarity
		if r.Score >= threshold {
			results = append(results, r)
		}
		if len(results) >= topK {
			break
		}
	}
	return results, rows.Err()
}
