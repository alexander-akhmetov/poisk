package store

import "fmt"

type Entry struct {
	Source    string
	FilePath  string
	LineNum   int
	EndLine   int
	Text      string
	Embedding []float32
	Folder    string
	Language  string
	Kind      string
	Symbol    string
}

func (s *Store) InsertEntries(source, filePath string, entries []Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete vec0 rows first (needs old IDs)
	if s.vecAvailable {
		if _, err := tx.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ? AND file_path = ?)",
			source, filePath,
		); err != nil {
			return fmt.Errorf("vec0 sync delete: %w", err)
		}
	}

	// Delete FTS5 rows
	if s.ftsAvailable {
		if _, err := tx.Exec(
			"DELETE FROM chunks_fts WHERE source = ? AND file_path = ?",
			source, filePath,
		); err != nil {
			return fmt.Errorf("FTS5 sync delete: %w", err)
		}
	}

	// Delete old embeddings
	if _, err := tx.Exec("DELETE FROM embeddings WHERE source = ? AND file_path = ?", source, filePath); err != nil {
		return fmt.Errorf("delete old entries: %w", err)
	}

	insertStmt, err := tx.Prepare(
		"INSERT INTO embeddings (source, file_path, line_num, chunk_text, embedding, folder, end_line, language, chunk_kind, symbol) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	for _, e := range entries {
		blob := Float32sToBlob(e.Embedding)
		res, err := insertStmt.Exec(source, filePath, e.LineNum, e.Text, blob, e.Folder, e.EndLine, e.Language, e.Kind, e.Symbol)
		if err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}

		if s.vecAvailable {
			if _, err := tx.Exec("INSERT INTO vec_embeddings (rowid, embedding) VALUES (?, ?)", rowid, blob); err != nil {
				return fmt.Errorf("vec0 sync insert: %w", err)
			}
		}

		if s.ftsAvailable {
			if _, err := tx.Exec(
				"INSERT INTO chunks_fts(chunk_text, id, source, file_path, line_num, folder, end_line, language, chunk_kind, symbol) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				e.Text, fmt.Sprintf("%d", rowid), source, filePath, fmt.Sprintf("%d", e.LineNum), e.Folder,
				fmt.Sprintf("%d", e.EndLine), e.Language, e.Kind, e.Symbol,
			); err != nil {
				return fmt.Errorf("FTS5 sync insert: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) Count(source string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM embeddings WHERE source = ?", source).Scan(&count)
	return count, err
}

func (s *Store) GetEntriesByPath(source, filePath string) ([]Entry, error) {
	rows, err := s.db.Query(
		`SELECT file_path, line_num, end_line, chunk_text, folder, language, chunk_kind, symbol
		FROM embeddings WHERE source = ? AND file_path = ? ORDER BY line_num`,
		source, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("get entries by path: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		e.Source = source
		if err := rows.Scan(&e.FilePath, &e.LineNum, &e.EndLine, &e.Text, &e.Folder, &e.Language, &e.Kind, &e.Symbol); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) ClearSource(source string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if s.vecAvailable {
		if _, err := tx.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ?)",
			source,
		); err != nil {
			return fmt.Errorf("clear vec_embeddings: %w", err)
		}
	}
	if s.ftsAvailable {
		if _, err := tx.Exec("DELETE FROM chunks_fts WHERE source = ?", source); err != nil {
			return fmt.Errorf("clear chunks_fts: %w", err)
		}
	}
	if _, err := tx.Exec("DELETE FROM embeddings WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embeddings: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM embedding_files WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embedding_files: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM embedding_meta WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embedding_meta: %w", err)
	}
	return tx.Commit()
}
