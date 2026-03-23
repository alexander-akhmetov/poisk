package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestValidateFolder(t *testing.T) {
	cfg := &config.Config{
		Folders: []config.FolderConfig{
			{Path: "/repo"},
			{Path: "/other"},
		},
	}
	svc := NewIndexService(nil, nil, cfg)

	tests := []struct {
		folder string
		want   bool
	}{
		{"/repo", true},
		{"/other", true},
		{"/unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.folder, func(t *testing.T) {
			if got := svc.ValidateFolder(tt.folder); got != tt.want {
				t.Errorf("ValidateFolder(%q) = %v, want %v", tt.folder, got, tt.want)
			}
		})
	}
}

func TestClearAllSources(t *testing.T) {
	t.Run("all succeed", func(t *testing.T) {
		store := newMockStore()
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/a"},
				{Path: "/b"},
			},
		}
		svc := NewIndexService(nil, store, cfg)

		store.addChunks("/a", "/a/f.go", nil)
		store.addChunks("/b", "/b/f.go", nil)

		if err := svc.ClearAllSources(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(store.tracked) != 0 {
			t.Errorf("expected all sources cleared, tracked has %d entries", len(store.tracked))
		}
	})

	t.Run("first fails short-circuits", func(t *testing.T) {
		store := newMockStore()
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/a"},
				{Path: "/b"},
			},
		}
		store.errClearSource = map[string]error{"/a": fmt.Errorf("disk error")}
		svc := NewIndexService(nil, store, cfg)

		store.addChunks("/b", "/b/f.go", nil)

		err := svc.ClearAllSources()
		if err == nil {
			t.Fatal("expected error")
		}
		// /b should still exist since /a failed first
		if _, ok := store.tracked["/b"]; !ok {
			t.Error("expected /b to still be tracked after /a failed")
		}
	})
}

func TestClearSource(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		Folders: []config.FolderConfig{{Path: "/repo"}},
	}
	svc := NewIndexService(nil, store, cfg)
	store.addChunks("/repo", "/repo/f.go", nil)

	if err := svc.ClearSource("/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := store.tracked["/repo"]; ok {
		t.Error("expected source to be cleared")
	}
}

func TestIndexServiceIntegration(t *testing.T) {
	dims := 256

	embSrv := newAppTestEmbeddingServer(t, dims)
	defer embSrv.Close()

	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "main.go"), []byte(`package main

func main() {
	fmt.Println("hello world")
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embSrv.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = dims
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: corpus, Description: "test"},
	}

	dbPath := filepath.Join(t.TempDir(), "index-svc.db")
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
	svc := NewIndexService(indexer, db, &cfg)

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "IndexAll indexes configured folders",
			run: func(t *testing.T) {
				stats, err := svc.IndexAll(context.Background())
				if err != nil {
					t.Fatalf("IndexAll: %v", err)
				}
				if len(stats) != 1 {
					t.Fatalf("expected 1 folder stat, got %d", len(stats))
				}
				if stats[0].Folder != corpus {
					t.Errorf("folder = %q, want %q", stats[0].Folder, corpus)
				}
				if stats[0].FilesProcessed == 0 && stats[0].FilesSkipped == 0 {
					t.Error("expected at least one file processed or skipped")
				}
			},
		},
		{
			name: "IndexFolder indexes single folder",
			run: func(t *testing.T) {
				stat, err := svc.IndexFolder(context.Background(), corpus)
				if err != nil {
					t.Fatalf("IndexFolder: %v", err)
				}
				if stat.Folder != corpus {
					t.Errorf("folder = %q, want %q", stat.Folder, corpus)
				}
			},
		},
		{
			name: "IndexFolder with invalid path returns error",
			run: func(t *testing.T) {
				_, err := svc.IndexFolder(context.Background(), "/nonexistent/path/xyz")
				if err == nil {
					t.Fatal("expected error for invalid path")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
