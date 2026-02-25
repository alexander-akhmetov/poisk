package app

import (
	"fmt"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func TestGetStatus(t *testing.T) {
	t.Run("single folder", func(t *testing.T) {
		store := newMockStore()
		store.vecAvailable = true
		store.ftsAvailable = true
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/repo", Description: "test repo"},
			},
		}
		store.addChunks("/repo", "/repo/a.go", []domain.Chunk{
			{Text: "chunk1"}, {Text: "chunk2"},
		})
		store.addChunks("/repo", "/repo/b.go", []domain.Chunk{
			{Text: "chunk3"},
		})

		svc := NewStatusService(store, cfg)
		status := svc.GetStatus()

		if !status.VecAvailable {
			t.Error("expected VecAvailable=true")
		}
		if !status.FTSAvailable {
			t.Error("expected FTSAvailable=true")
		}
		if len(status.Folders) != 1 {
			t.Fatalf("got %d folders, want 1", len(status.Folders))
		}
		f := status.Folders[0]
		if f.Path != "/repo" {
			t.Errorf("Path = %q, want %q", f.Path, "/repo")
		}
		if f.Description != "test repo" {
			t.Errorf("Description = %q, want %q", f.Description, "test repo")
		}
		if f.Files != 2 {
			t.Errorf("Files = %d, want 2", f.Files)
		}
		if f.Chunks != 3 {
			t.Errorf("Chunks = %d, want 3", f.Chunks)
		}
	})

	t.Run("multiple folders", func(t *testing.T) {
		store := newMockStore()
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/a", Description: "first"},
				{Path: "/b", Description: "second"},
			},
		}
		store.addChunks("/a", "/a/f.go", []domain.Chunk{{Text: "x"}})

		svc := NewStatusService(store, cfg)
		status := svc.GetStatus()

		if len(status.Folders) != 2 {
			t.Fatalf("got %d folders, want 2", len(status.Folders))
		}
		if status.Folders[0].Chunks != 1 {
			t.Errorf("folder /a chunks = %d, want 1", status.Folders[0].Chunks)
		}
		if status.Folders[1].Chunks != 0 {
			t.Errorf("folder /b chunks = %d, want 0", status.Folders[1].Chunks)
		}
	})

	t.Run("vec and fts flags", func(t *testing.T) {
		store := newMockStore()
		store.vecAvailable = false
		store.ftsAvailable = false
		cfg := &config.Config{}

		svc := NewStatusService(store, cfg)
		status := svc.GetStatus()

		if status.VecAvailable {
			t.Error("expected VecAvailable=false")
		}
		if status.FTSAvailable {
			t.Error("expected FTSAvailable=false")
		}
	})

	t.Run("context propagated", func(t *testing.T) {
		store := newMockStore()
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/repo", Context: map[string]string{"src/": "source code"}},
			},
		}
		svc := NewStatusService(store, cfg)
		status := svc.GetStatus()

		if len(status.Folders) != 1 {
			t.Fatalf("got %d folders, want 1", len(status.Folders))
		}
		if status.Folders[0].Context == nil {
			t.Fatal("expected context to be set")
		}
		if status.Folders[0].Context["src/"] != "source code" {
			t.Errorf("context = %v, want src/ -> source code", status.Folders[0].Context)
		}
	})

	t.Run("store error logged gracefully", func(t *testing.T) {
		store := newMockStore()
		store.errCount = fmt.Errorf("count error")
		store.errTrackedCount = fmt.Errorf("tracked count error")
		cfg := &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/repo"},
			},
		}
		svc := NewStatusService(store, cfg)

		// Should not panic; errors are logged, not returned.
		status := svc.GetStatus()
		if len(status.Folders) != 1 {
			t.Fatalf("got %d folders, want 1", len(status.Folders))
		}
		if status.Folders[0].Chunks != 0 {
			t.Errorf("chunks = %d, want 0 on error", status.Folders[0].Chunks)
		}
		if status.Folders[0].Files != 0 {
			t.Errorf("files = %d, want 0 on error", status.Folders[0].Files)
		}
	})
}
