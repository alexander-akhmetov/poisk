package store

import (
	"fmt"
	"log/slog"
)

type Entry struct {
	Source   string
	FilePath string
	LineNum  int
	Text     string
	Embedding []float32
	Folder   string
}

func (s *Store) InsertEntries(source, filePath string, entries []Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete vec0 rows first (needs old IDs)
	if s.vecAvailable {
		if _, err := tx.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ? AND file_path = ?)",
			source, filePath,
		); err != nil {
			slog.Warn("vec0 sync delete failed", "error", err)
		}
	}

	// Delete FTS5 rows
	if s.ftsAvailable {
		if _, err := tx.Exec(
			"DELETE FROM chunks_fts WHERE source = ? AND file_path = ?",
			source, filePath,
		); err != nil {
			slog.Warn("FTS5 sync delete failed", "error", err)
		}
	}

	// Delete old embeddings
	if _, err := tx.Exec("DELETE FROM embeddings WHERE source = ? AND file_path = ?", source, filePath); err != nil {
		return fmt.Errorf("delete old entries: %w", err)
	}

	insertStmt, err := tx.Prepare(
		"INSERT INTO embeddings (source, file_path, line_num, chunk_text, embedding, folder) VALUES (?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	for _, e := range entries {
		blob := Float32sToBlob(e.Embedding)
		res, err := insertStmt.Exec(source, filePath, e.LineNum, e.Text, blob, e.Folder)
		if err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
		rowid, _ := res.LastInsertId()

		if s.vecAvailable {
			if _, err := tx.Exec("INSERT INTO vec_embeddings (rowid, embedding) VALUES (?, ?)", rowid, blob); err != nil {
				slog.Warn("vec0 sync insert failed", "error", err)
			}
		}

		if s.ftsAvailable {
			if _, err := tx.Exec(
				"INSERT INTO chunks_fts(chunk_text, id, source, file_path, line_num, folder) VALUES (?, ?, ?, ?, ?, ?)",
				e.Text, fmt.Sprintf("%d", rowid), source, filePath, fmt.Sprintf("%d", e.LineNum), e.Folder,
			); err != nil {
				slog.Warn("FTS5 sync insert failed", "error", err)
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
		s.db.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ?)",
			source,
		)
	}
	if s.ftsAvailable {
		s.db.Exec("DELETE FROM chunks_fts WHERE source = ?", source)
	}
	s.db.Exec("DELETE FROM embeddings WHERE source = ?", source)
	s.db.Exec("DELETE FROM embedding_files WHERE source = ?", source)
	s.db.Exec("DELETE FROM embedding_meta WHERE source = ?", source)
	return nil
}
