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
		rowid, _ := res.LastInsertId()

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

func (s *Store) ClearSource(source string) error {
	if s.vecAvailable {
		if _, err := s.db.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ?)",
			source,
		); err != nil {
			return fmt.Errorf("clear vec_embeddings: %w", err)
		}
	}
	if s.ftsAvailable {
		if _, err := s.db.Exec("DELETE FROM chunks_fts WHERE source = ?", source); err != nil {
			return fmt.Errorf("clear chunks_fts: %w", err)
		}
	}
	if _, err := s.db.Exec("DELETE FROM embeddings WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embeddings: %w", err)
	}
	if _, err := s.db.Exec("DELETE FROM embedding_files WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embedding_files: %w", err)
	}
	if _, err := s.db.Exec("DELETE FROM embedding_meta WHERE source = ?", source); err != nil {
		return fmt.Errorf("clear embedding_meta: %w", err)
	}
	return nil
}
