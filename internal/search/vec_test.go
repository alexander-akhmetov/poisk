package search

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/domain"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestExecVecQueryAppliesFolderMetadataFiltersAndThreshold(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec-filters.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	if err := s.InsertChunks("repo-a", "target.go", []domain.ChunkWithEmbedding{
		{Chunk: domain.Chunk{LineNum: 10, EndLine: 20, Text: "fetch user by id", Folder: "repo-a", Language: "go", Kind: "function_declaration", Symbol: "FetchUser"}, Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatalf("insert target entry: %v", err)
	}
	if err := s.InsertChunks("repo-b", "other-source.go", []domain.ChunkWithEmbedding{
		{Chunk: domain.Chunk{LineNum: 10, EndLine: 20, Text: "fetch user by id", Folder: "repo-b", Language: "go", Kind: "function_declaration", Symbol: "FetchUser"}, Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatalf("insert cross-folder noise entry: %v", err)
	}
	if err := s.InsertChunks("repo-a", "wrong-language.go", []domain.ChunkWithEmbedding{
		{Chunk: domain.Chunk{LineNum: 10, EndLine: 20, Text: "fetch user by id", Folder: "repo-a", Language: "rust", Kind: "function_item", Symbol: "fetch_user"}, Embedding: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatalf("insert metadata noise entry: %v", err)
	}

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	results, err := execVecQuery(s, queryBlob, 5, 50, []string{"repo-a"}, MetadataFilters{
		Languages: []string{"go"},
		Kinds:     []string{"function_declaration"},
		Symbols:   []string{"fetchuser"},
	}, 0.9)
	if err != nil {
		t.Fatalf("execVecQuery failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one filtered vec result, got %d", len(results))
	}
	if results[0].FilePath != "target.go" {
		t.Fatalf("expected target.go, got %s", results[0].FilePath)
	}
	if results[0].Language != "go" || results[0].Kind != "function_declaration" || results[0].Symbol != "FetchUser" {
		t.Fatalf("unexpected metadata in vec result: %+v", results[0])
	}
	if results[0].Score < 0.9 {
		t.Fatalf("expected vec score >= 0.9, got %.3f", results[0].Score)
	}
}

func TestSearchVecRetriesWhenFilteredResultsAreBelowTopK(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec-retry.db")
	s, err := store.Open(dbPath, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	for i := range 15 {
		if err := s.InsertChunks("repo", fmt.Sprintf("noise-%02d.go", i), []domain.ChunkWithEmbedding{
			{Chunk: domain.Chunk{LineNum: 1, EndLine: 1, Text: "python helper", Folder: "repo", Language: "python", Kind: "function_definition", Symbol: fmt.Sprintf("noise%d", i)}, Embedding: []float32{1, 0, 0}},
		}); err != nil {
			t.Fatalf("insert noise entry %d: %v", i, err)
		}
	}
	if err := s.InsertChunks("repo", "target.go", []domain.ChunkWithEmbedding{
		{Chunk: domain.Chunk{LineNum: 1, EndLine: 1, Text: "go helper", Folder: "repo", Language: "go", Kind: "function_declaration", Symbol: "target"}, Embedding: []float32{0.8, 0.2, 0}},
	}); err != nil {
		t.Fatalf("insert target entry: %v", err)
	}

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	results, err := searchVec(s, queryBlob, 3, nil, MetadataFilters{
		Languages: []string{"go"},
	}, 0.0)
	if err != nil {
		t.Fatalf("searchVec failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one go result after filtered retry, got %d", len(results))
	}
	if results[0].Language != "go" || results[0].FilePath != "target.go" {
		t.Fatalf("unexpected retry result: %+v", results[0])
	}
}
