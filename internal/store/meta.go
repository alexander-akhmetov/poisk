package store

import (
	"database/sql"
	"errors"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func (s *Store) ModelChanged(source, model string, dimensions int) (domain.ModelChange, error) {
	var mc domain.ModelChange
	err := s.db.QueryRow(
		"SELECT model, dimensions FROM embedding_meta WHERE source = ?", source,
	).Scan(&mc.OldModel, &mc.OldDims)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ModelChange{Changed: true}, nil
		}
		return mc, err
	}
	mc.Changed = mc.OldModel != model || mc.OldDims != dimensions
	return mc, nil
}

func (s *Store) AllSources() ([]string, error) {
	rows, err := s.db.Query("SELECT source FROM embedding_meta")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) UpdateMeta(source, model string, dimensions int) error {
	_, err := s.db.Exec(
		`INSERT INTO embedding_meta (source, model, dimensions) VALUES (?, ?, ?)
		 ON CONFLICT(source) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions`,
		source, model, dimensions,
	)
	return err
}
