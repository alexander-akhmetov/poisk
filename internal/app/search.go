package app

import (
	"context"

	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/search"
)

type SearchService struct {
	searcher *search.Searcher
}

func NewSearchService(searcher *search.Searcher) *SearchService {
	return &SearchService{searcher: searcher}
}

func (s *SearchService) Search(ctx context.Context, query string, topK int, folders []string) ([]domain.SearchResult, error) {
	return s.searcher.Search(ctx, query, topK, folders)
}
