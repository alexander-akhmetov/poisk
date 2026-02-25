package app

import (
	"context"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/domain"
	"github.com/akhmetov/poisk/internal/index"
	"github.com/akhmetov/poisk/internal/ports"
)

type IndexService struct {
	indexer *index.Indexer
	store   ports.ChunkStore
	cfg     *config.Config
}

func NewIndexService(indexer *index.Indexer, store ports.ChunkStore, cfg *config.Config) *IndexService {
	return &IndexService{indexer: indexer, store: store, cfg: cfg}
}

func (s *IndexService) IndexAll(ctx context.Context) ([]domain.FolderStats, error) {
	stats, err := s.indexer.IndexAll(ctx)
	if err != nil {
		return nil, err
	}
	return convertStats(stats), nil
}

func (s *IndexService) IndexFolder(ctx context.Context, folder string) (domain.FolderStats, error) {
	stats, err := s.indexer.IndexFolder(ctx, folder)
	if err != nil {
		return domain.FolderStats{}, err
	}
	return convertStat(stats), nil
}

func (s *IndexService) ValidateFolder(folder string) bool {
	for _, f := range s.cfg.Folders {
		if f.Path == folder {
			return true
		}
	}
	return false
}

func (s *IndexService) ClearSource(source string) error {
	return s.store.ClearSource(source)
}

func (s *IndexService) ClearAllSources() error {
	for _, f := range s.cfg.Folders {
		if err := s.store.ClearSource(f.Path); err != nil {
			return err
		}
	}
	return nil
}

func convertStats(stats []index.FolderStats) []domain.FolderStats {
	result := make([]domain.FolderStats, len(stats))
	for i, s := range stats {
		result[i] = convertStat(s)
	}
	return result
}

func convertStat(s index.FolderStats) domain.FolderStats {
	return domain.FolderStats{
		Folder:                 s.Folder,
		FilesProcessed:         s.FilesProcessed,
		FilesSkipped:           s.FilesSkipped,
		ChunksCreated:          s.ChunksCreated,
		Errors:                 s.Errors,
		FilesSkippedParseError: s.FilesSkippedParseError,
	}
}
