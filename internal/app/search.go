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
	results, err := s.searcher.Search(ctx, query, topK, folders)
	if err != nil && len(results) == 0 {
		return nil, err
	}
	domainResults := make([]domain.SearchResult, len(results))
	for i, r := range results {
		domainResults[i] = domain.SearchResult{
			FilePath: r.FilePath,
			LineNum:  r.LineNum,
			EndLine:  r.EndLine,
			Text:     r.Text,
			Score:    r.Score,
			Folder:   r.Folder,
			Language: r.Language,
			Kind:     r.Kind,
			Symbol:   r.Symbol,
			Context:  r.Context,
		}
	}
	return domainResults, err
}
