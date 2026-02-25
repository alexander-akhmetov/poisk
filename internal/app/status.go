package app

import (
	"log/slog"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/domain"
	"github.com/akhmetov/poisk/internal/ports"
)

type StatusService struct {
	store ports.ChunkStore
	cfg   *config.Config
}

func NewStatusService(store ports.ChunkStore, cfg *config.Config) *StatusService {
	return &StatusService{store: store, cfg: cfg}
}

func (s *StatusService) GetStatus() domain.IndexStatus {
	status := domain.IndexStatus{
		VecAvailable: s.store.VecAvailable(),
		FTSAvailable: s.store.FTSAvailable(),
	}
	for _, f := range s.cfg.Folders {
		count, err := s.store.Count(f.Path)
		if err != nil {
			slog.Error("status: count chunks failed", "source", f.Path, "error", err)
		}
		fileCount, err := s.store.TrackedFileCount(f.Path)
		if err != nil {
			slog.Error("status: count files failed", "source", f.Path, "error", err)
		}
		fs := domain.FolderStatus{
			Path:        f.Path,
			Description: f.Description,
			Files:       fileCount,
			Chunks:      count,
		}
		if len(f.Context) > 0 {
			fs.Context = f.Context
		}
		status.Folders = append(status.Folders, fs)
	}
	return status
}
