package store

import (
	"database/sql"
	"errors"
)

func (s *Store) ModelChanged(source, model string, dimensions int) (bool, error) {
	var storedModel string
	var storedDims int
	err := s.db.QueryRow(
		"SELECT model, dimensions FROM embedding_meta WHERE source = ?", source,
	).Scan(&storedModel, &storedDims)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	return storedModel != model || storedDims != dimensions, nil
}

func (s *Store) UpdateMeta(source, model string, dimensions int) error {
	_, err := s.db.Exec(
		`INSERT INTO embedding_meta (source, model, dimensions) VALUES (?, ?, ?)
		 ON CONFLICT(source) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions`,
		source, model, dimensions,
	)
	return err
}
