package store

import (
	"database/sql"
	"errors"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func (s *Store) ModelChanged(source, model string, dimensions int, quantization string) (domain.ModelChange, error) {
	var mc domain.ModelChange
	err := s.db.QueryRow(
		"SELECT model, dimensions, quantization FROM embedding_meta WHERE source = ?", source,
	).Scan(&mc.OldModel, &mc.OldDims, &mc.OldQuantization)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ModelChange{Changed: true}, nil
		}
		return mc, err
	}
	mc.Changed = mc.OldModel != model || mc.OldDims != dimensions || mc.OldQuantization != quantization
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

func (s *Store) UpdateMeta(source, model string, dimensions int, quantization string) error {
	_, err := s.db.Exec(
		`INSERT INTO embedding_meta (source, model, dimensions, quantization) VALUES (?, ?, ?, ?)
		 ON CONFLICT(source) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions, quantization = excluded.quantization`,
		source, model, dimensions, quantization,
	)
	return err
}
