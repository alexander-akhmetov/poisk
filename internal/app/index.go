package app

import (
	"context"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/ports"
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
	return s.indexer.IndexAll(ctx)
}

func (s *IndexService) IndexFolder(ctx context.Context, folder string) (domain.FolderStats, error) {
	return s.indexer.IndexFolder(ctx, folder)
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
