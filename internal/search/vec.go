package search

import (
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/store"
)

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
	if len(filters.Languages) > 0 {
		placeholders := strings.Repeat("?,", len(filters.Languages))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND e.language IN (" + placeholders + ")"
		for _, v := range filters.Languages {
			args = append(args, v)
		}
	}
	if len(filters.Kinds) > 0 {
		placeholders := strings.Repeat("?,", len(filters.Kinds))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND e.chunk_kind IN (" + placeholders + ")"
		for _, v := range filters.Kinds {
			args = append(args, v)
		}
	}
	if len(filters.Symbols) > 0 {
		placeholders := strings.Repeat("?,", len(filters.Symbols))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND LOWER(e.symbol) IN (" + placeholders + ")"
		for _, v := range filters.Symbols {
			args = append(args, v)
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
