package store

import (
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) GetFileMtime(source, filePath string) (int64, bool, error) {
	var mtime int64
	err := s.db.QueryRow(
		"SELECT mtime FROM embedding_files WHERE source = ? AND file_path = ?",
		source, filePath,
	).Scan(&mtime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return mtime, true, nil
}

func (s *Store) SetFileMtime(source, filePath string, mtime int64) error {
	_, err := s.db.Exec(
		`INSERT INTO embedding_files (source, file_path, mtime) VALUES (?, ?, ?)
		 ON CONFLICT(source, file_path) DO UPDATE SET mtime = excluded.mtime`,
		source, filePath, mtime,
	)
	return err
}

func (s *Store) DeleteFile(source, filePath string) error {
	if s.vecAvailable {
		if _, err := s.db.Exec(
			"DELETE FROM vec_embeddings WHERE rowid IN (SELECT id FROM embeddings WHERE source = ? AND file_path = ?)",
			source, filePath,
		); err != nil {
			return fmt.Errorf("delete vec_embeddings: %w", err)
		}
	}
	if s.ftsAvailable {
		if _, err := s.db.Exec(
			"DELETE FROM chunks_fts WHERE source = ? AND file_path = ?",
			source, filePath,
		); err != nil {
			return fmt.Errorf("delete chunks_fts: %w", err)
		}
	}
	if _, err := s.db.Exec("DELETE FROM embeddings WHERE source = ? AND file_path = ?", source, filePath); err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	_, err := s.db.Exec("DELETE FROM embedding_files WHERE source = ? AND file_path = ?", source, filePath)
	return err
}

func (s *Store) TrackedFiles(source string) (map[string]int64, error) {
	rows, err := s.db.Query("SELECT file_path, mtime FROM embedding_files WHERE source = ?", source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]int64)
	for rows.Next() {
		var fp string
		var mt int64
		if err := rows.Scan(&fp, &mt); err != nil {
			return nil, err
		}
		m[fp] = mt
	}
	return m, rows.Err()
}

func (s *Store) TrackedFilePaths(source string) ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT file_path FROM embedding_files WHERE source = ? ORDER BY file_path", source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, err
		}
		paths = append(paths, fp)
	}
	return paths, rows.Err()
}

func (s *Store) TrackedFileCount(source string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM embedding_files WHERE source = ?", source).Scan(&count)
	return count, err
}
