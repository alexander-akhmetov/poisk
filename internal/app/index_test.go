package app

import (
	"fmt"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/index"
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

func TestConvertStats(t *testing.T) {
	in := []index.FolderStats{
		{Folder: "/a", FilesProcessed: 1},
		{Folder: "/b", FilesProcessed: 2},
	}
	out := convertStats(in)
	if len(out) != 2 {
		t.Fatalf("got %d, want 2", len(out))
	}
	if out[0].Folder != "/a" || out[1].Folder != "/b" {
		t.Errorf("folders = %q, %q", out[0].Folder, out[1].Folder)
	}
}

func TestConvertStat(t *testing.T) {
	in := index.FolderStats{
		Folder:                 "/repo",
		FilesProcessed:         10,
		FilesSkipped:           2,
		ChunksCreated:          50,
		Errors:                 1,
		FilesSkippedParseError: 3,
	}
	out := convertStat(in)

	if out.Folder != in.Folder {
		t.Errorf("Folder = %q, want %q", out.Folder, in.Folder)
	}
	if out.FilesProcessed != in.FilesProcessed {
		t.Errorf("FilesProcessed = %d, want %d", out.FilesProcessed, in.FilesProcessed)
	}
	if out.FilesSkipped != in.FilesSkipped {
		t.Errorf("FilesSkipped = %d, want %d", out.FilesSkipped, in.FilesSkipped)
	}
	if out.ChunksCreated != in.ChunksCreated {
		t.Errorf("ChunksCreated = %d, want %d", out.ChunksCreated, in.ChunksCreated)
	}
	if out.Errors != in.Errors {
		t.Errorf("Errors = %d, want %d", out.Errors, in.Errors)
	}
	if out.FilesSkippedParseError != in.FilesSkippedParseError {
		t.Errorf("FilesSkippedParseError = %d, want %d", out.FilesSkippedParseError, in.FilesSkippedParseError)
	}
}
