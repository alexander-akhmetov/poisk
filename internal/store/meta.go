package store

import (
	"database/sql"
	"errors"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func (s *Store) ModelChanged(source, model string, dimensions int, quantization string, maxInputBytes int) (domain.ModelChange, error) {
	var mc domain.ModelChange
	err := s.db.QueryRow(
		"SELECT model, dimensions, quantization, max_input_bytes FROM embedding_meta WHERE source = ?", source,
	).Scan(&mc.OldModel, &mc.OldDims, &mc.OldQuantization, &mc.OldMaxInputBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ModelChange{Changed: true}, nil
		}
		return mc, err
	}
	// A different input limit means the source was chunked to other
	// boundaries, so its stored text no longer matches what the config asks
	// for, whatever the file mtimes say.
	mc.Changed = mc.OldModel != model || mc.OldDims != dimensions ||
		mc.OldQuantization != quantization || mc.OldMaxInputBytes != maxInputBytes
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

func (s *Store) UpdateMeta(source, model string, dimensions int, quantization string, maxInputBytes int) error {
	_, err := s.db.Exec(
		`INSERT INTO embedding_meta (source, model, dimensions, quantization, max_input_bytes) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(source) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions,
		 quantization = excluded.quantization, max_input_bytes = excluded.max_input_bytes`,
		source, model, dimensions, quantization, maxInputBytes,
	)
	return err
}
