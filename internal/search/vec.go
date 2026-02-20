package search

import (
	"github.com/akhmetov/poisk/internal/store"
)

func searchVec(s *store.Store, queryBlob []byte, topK int, folder string, threshold float64) ([]Result, error) {
	if !s.VecAvailable() {
		return nil, nil
	}

	fetchLimit := topK * 5

	query := `SELECT e.file_path, e.line_num, e.chunk_text, v.distance, e.folder
		FROM vec_embeddings v
		JOIN embeddings e ON e.id = v.rowid
		WHERE v.embedding MATCH ? AND k = ?`
	args := []any{queryBlob, fetchLimit}

	if folder != "" {
		query += " AND e.source = ?"
		args = append(args, folder)
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
		if err := rows.Scan(&r.FilePath, &r.LineNum, &r.Text, &distance, &r.Folder); err != nil {
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
