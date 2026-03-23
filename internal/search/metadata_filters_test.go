package search

import (
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestSearchFTSMetadataFilters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search-filters.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	if err := s.InsertChunks("src", "main.go", []domain.ChunkWithEmbedding{
		{
			Chunk:     domain.Chunk{LineNum: 10, EndLine: 20, Text: "retry with exponential backoff and jitter", Folder: "src", Language: "go", Kind: "function_declaration", Symbol: "FetchUser"},
			Embedding: []float32{1, 0, 0},
		},
		{
			Chunk:     domain.Chunk{LineNum: 30, EndLine: 40, Text: "retry with fixed delay", Folder: "src", Language: "python", Kind: "function_definition", Symbol: "fetch_user"},
			Embedding: []float32{0, 1, 0},
		},
		{
			Chunk:     domain.Chunk{LineNum: 50, EndLine: 60, Text: "database connection bootstrap", Folder: "src", Language: "rust", Kind: "function_item", Symbol: "connect_db"},
			Embedding: []float32{0, 0, 1},
		},
	}); err != nil {
		t.Fatalf("insert entries: %v", err)
	}

	results, err := searchFTS(s, "retry", 10, nil, MetadataFilters{
		Languages: []string{"go"},
		Kinds:     []string{"function_declaration"},
		Symbols:   []string{"fetchuser"},
	})
	if err != nil {
		t.Fatalf("search with metadata filters: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one filtered result, got %d", len(results))
	}
	if results[0].Language != "go" || results[0].Kind != "function_declaration" || results[0].Symbol != "FetchUser" {
		t.Fatalf("unexpected filtered result: %+v", results[0])
	}

	filterOnly, err := searchFTS(s, "", 10, nil, MetadataFilters{
		Languages: []string{"rust"},
	})
	if err != nil {
		t.Fatalf("filter-only search failed: %v", err)
	}
	if len(filterOnly) != 1 {
		t.Fatalf("expected one result for filter-only query, got %d", len(filterOnly))
	}
	if filterOnly[0].Language != "rust" {
		t.Fatalf("expected rust result, got %+v", filterOnly[0])
	}
}
