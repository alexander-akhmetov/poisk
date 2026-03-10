package store

import "github.com/alexander-akhmetov/poisk/internal/domain"

func (s *Store) SetIndexingProgress(folder string, total, processed int, startedAt int64) error {
	_, err := s.db.Exec(
		`INSERT INTO indexing_progress (folder, total, processed, started_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(folder) DO UPDATE SET total = excluded.total, processed = excluded.processed, started_at = excluded.started_at`,
		folder, total, processed, startedAt,
	)
	return err
}

func (s *Store) UpdateIndexingProcessed(folder string, processed int) error {
	_, err := s.db.Exec(
		"UPDATE indexing_progress SET processed = ? WHERE folder = ?",
		processed, folder,
	)
	return err
}

func (s *Store) ClearIndexingProgress(folder string) error {
	_, err := s.db.Exec("DELETE FROM indexing_progress WHERE folder = ?", folder)
	return err
}

func (s *Store) GetIndexingProgress() ([]domain.IndexingProgress, error) {
	rows, err := s.db.Query("SELECT folder, total, processed, started_at FROM indexing_progress")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.IndexingProgress
	for rows.Next() {
		var p domain.IndexingProgress
		if err := rows.Scan(&p.Folder, &p.Total, &p.Processed, &p.StartedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
