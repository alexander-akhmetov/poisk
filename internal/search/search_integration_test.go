package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestSearchIntegration(t *testing.T) {
	ctx := context.Background()
	dims := 256

	// Create two separate corpus directories for folder filtering tests.
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	goFile := filepath.Join(dir1, "handler.go")
	mdFile := filepath.Join(dir1, "notes.md")
	pyFile := filepath.Join(dir2, "util.py")

	if err := os.WriteFile(goFile, []byte(`package handler

// HandleRequest processes incoming HTTP requests
func HandleRequest(w http.ResponseWriter, r *http.Request) {
	uniquetoken := r.URL.Query().Get("id")
	fmt.Fprintf(w, "hello %s", uniquetoken)
}
`), 0o644); err != nil {
		t.Fatalf("write go fixture: %v", err)
	}

	if err := os.WriteFile(mdFile, []byte(`# Architecture Notes

The system uses a layered architecture with handlers, services, and repositories.
`), 0o644); err != nil {
		t.Fatalf("write md fixture: %v", err)
	}

	if err := os.WriteFile(pyFile, []byte(`def process_data(items):
    """Process a list of data items and return filtered results."""
    return [item for item in items if item.is_valid()]
`), 0o644); err != nil {
		t.Fatalf("write py fixture: %v", err)
	}

	embeddingServer := newEmbeddingServer(t, dims)
	defer embeddingServer.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embeddingServer.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Search.SimilarityThreshold = 0.0
	cfg.Search.MinScore = 0
	cfg.Search.DefaultTopK = 20
	cfg.Folders = []config.FolderConfig{
		{Path: dir1, Description: "corpus-1"},
		{Path: dir2, Description: "corpus-2"},
	}

	dbPath := filepath.Join(t.TempDir(), "integration.db")
	db, err := store.Open(dbPath, dims)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if !db.VecAvailable() {
		t.Skip("vec0 not available")
	}

	embedClient := embed.NewClient(
		cfg.Embedding.BaseURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.SendDimensions,
	)
	indexer := index.NewIndexer(db, embedClient, &cfg)
	if _, err := indexer.IndexAll(ctx); err != nil {
		t.Fatalf("index fixtures: %v", err)
	}

	searcher := NewSearcher(db, embedClient, &cfg, nil)

	tests := []struct {
		name    string
		query   string
		topK    int
		folders []string
		setup   func()
		check   func(t *testing.T, results []Result, err error)
	}{
		{
			name:  "hybrid_basic",
			query: "HandleRequest",
			topK:  5,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected results from hybrid search")
				}
			},
		},
		{
			name:  "fts_only",
			query: "lex:uniquetoken",
			topK:  5,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected FTS results for unique keyword")
				}
				for _, r := range results {
					if filepath.Base(r.FilePath) != "handler.go" {
						t.Errorf("expected only handler.go results, got %s", r.FilePath)
					}
				}
			},
		},
		{
			name:  "vec_only",
			query: "vec:process data items",
			topK:  5,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected vec results for semantic query")
				}
			},
		},
		{
			name:  "pipe_composition",
			query: "lex:HandleRequest | vec:process data",
			topK:  10,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected results from pipe composition")
				}
			},
		},
		{
			name:  "empty_query",
			query: "",
			topK:  5,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Errorf("expected nil error for empty query, got %v", err)
				}
				if results != nil {
					t.Errorf("expected nil results for empty query, got %d", len(results))
				}
			},
		},
		{
			name:  "topK_respected",
			query: "function",
			topK:  1,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) > 1 {
					t.Errorf("expected at most 1 result, got %d", len(results))
				}
			},
		},
		{
			name:    "folder_filter",
			query:   "lex:process",
			topK:    5,
			folders: []string{dir2},
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected results from folder filter")
				}
				for _, r := range results {
					if r.Folder != dir2 {
						t.Errorf("expected only dir2 results, got folder %s", r.Folder)
					}
				}
			},
		},
		{
			name:  "lang_filter",
			query: "lex:process lang:python",
			topK:  5,
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) == 0 {
					t.Fatal("expected results from lang filter")
				}
				for _, r := range results {
					if r.Language != "python" {
						t.Errorf("expected only python results, got language %q", r.Language)
					}
				}
			},
		},
		{
			name:  "min_score",
			query: "handler",
			topK:  5,
			setup: func() {
				cfg.Search.MinScore = 999
			},
			check: func(t *testing.T, results []Result, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(results) != 0 {
					t.Errorf("expected 0 results with very high min_score, got %d", len(results))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset min_score to 0 before each test
			cfg.Search.MinScore = 0
			if tt.setup != nil {
				tt.setup()
			}
			results, err := searcher.Search(ctx, tt.query, tt.topK, tt.folders)
			tt.check(t, results, err)
		})
	}
}
