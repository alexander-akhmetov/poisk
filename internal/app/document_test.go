package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/domain"
)

func TestGetDocument(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		Folders: []config.FolderConfig{
			{Path: "/repo", Context: map[string]string{"src": "source code"}},
		},
	}
	svc := NewDocumentService(store, cfg)

	store.addChunks("/repo", "/repo/src/main.go", []domain.Chunk{
		{FilePath: "/repo/src/main.go", LineNum: 1, EndLine: 10, Text: "package main"},
		{FilePath: "/repo/src/main.go", LineNum: 11, EndLine: 20, Text: "func main()"},
		{FilePath: "/repo/src/main.go", LineNum: 21, EndLine: 30, Text: "// end"},
	})

	tests := []struct {
		name    string
		path    string
		start   int
		end     int
		wantN   int
		wantCtx bool
		wantErr bool
	}{
		{
			name:    "all chunks",
			path:    "/repo/src/main.go",
			wantN:   3,
			wantCtx: true,
		},
		{
			name:    "line range filtering",
			path:    "/repo/src/main.go",
			start:   5,
			end:     15,
			wantN:   2,
			wantCtx: true,
		},
		{
			name:    "start line only",
			path:    "/repo/src/main.go",
			start:   15,
			wantN:   2,
			wantCtx: true,
		},
		{
			name:    "end line only",
			path:    "/repo/src/main.go",
			end:     10,
			wantN:   1,
			wantCtx: true,
		},
		{
			name:    "file not under any folder",
			path:    "/other/file.go",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, ctx, err := svc.GetDocument(tt.path, tt.start, tt.end)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(chunks) != tt.wantN {
				t.Errorf("got %d chunks, want %d", len(chunks), tt.wantN)
			}
			if tt.wantCtx && len(ctx) == 0 {
				t.Error("expected context annotations")
			}
		})
	}
}

func TestGetDocumentStoreError(t *testing.T) {
	store := newMockStore()
	store.errGetChunks = fmt.Errorf("db read error")
	cfg := &config.Config{
		Folders: []config.FolderConfig{{Path: "/repo"}},
	}
	svc := NewDocumentService(store, cfg)

	_, _, err := svc.GetDocument("/repo/file.go", 0, 0)
	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestGetDocumentNoContext(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		Folders: []config.FolderConfig{{Path: "/repo"}},
	}
	svc := NewDocumentService(store, cfg)

	store.addChunks("/repo", "/repo/file.go", []domain.Chunk{
		{FilePath: "/repo/file.go", LineNum: 1, EndLine: 5, Text: "hello"},
	})

	_, ctx, err := svc.GetDocument("/repo/file.go", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx) != 0 {
		t.Errorf("expected no context, got %v", ctx)
	}
}

func TestGetMultipleDocuments(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		Folders: []config.FolderConfig{
			{Path: "/repo"},
		},
	}
	svc := NewDocumentService(store, cfg)

	store.addChunks("/repo", "/repo/main.go", []domain.Chunk{
		{FilePath: "/repo/main.go", LineNum: 1, EndLine: 5, Text: "package main"},
	})
	store.addChunks("/repo", "/repo/util.go", []domain.Chunk{
		{FilePath: "/repo/util.go", LineNum: 1, EndLine: 3, Text: "package util"},
	})

	t.Run("exact paths", func(t *testing.T) {
		results, truncated, err := svc.GetMultipleDocuments("/repo/main.go,/repo/util.go", 100_000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if truncated {
			t.Error("unexpected truncation")
		}
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}
	})

	t.Run("glob matching", func(t *testing.T) {
		results, _, err := svc.GetMultipleDocuments("*.go", 100_000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) < 2 {
			t.Fatalf("got %d results, want at least 2", len(results))
		}
	})

	t.Run("dedup", func(t *testing.T) {
		results, _, err := svc.GetMultipleDocuments("/repo/main.go,/repo/main.go", 100_000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1 (dedup)", len(results))
		}
	})

	t.Run("byte budget truncation", func(t *testing.T) {
		_, truncated, err := svc.GetMultipleDocuments("/repo/main.go,/repo/util.go", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !truncated {
			t.Error("expected truncation with tiny byte budget")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		results, truncated, err := svc.GetMultipleDocuments("", 100_000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if truncated {
			t.Error("unexpected truncation")
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results for empty input, got %d", len(results))
		}
	})

	t.Run("default maxBytes", func(t *testing.T) {
		results, _, err := svc.GetMultipleDocuments("/repo/main.go", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1", len(results))
		}
	})
}

func TestEffectiveMaxBytes(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int
		want     int
	}{
		{name: "unset takes the default", maxBytes: 0, want: defaultMaxBytes},
		{name: "negative takes the default", maxBytes: -1, want: defaultMaxBytes},
		{name: "ordinary value passes through", maxBytes: 50_000, want: 50_000},
		{name: "exactly at the ceiling", maxBytes: maxMaxBytes, want: maxMaxBytes},
		{name: "one above the ceiling", maxBytes: maxMaxBytes + 1, want: maxMaxBytes},
		{name: "far above the ceiling", maxBytes: 50_000_000, want: maxMaxBytes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveMaxBytes(tt.maxBytes)
			if got != tt.want {
				t.Fatalf("EffectiveMaxBytes(%d) = %d, want %d", tt.maxBytes, got, tt.want)
			}
			if got > maxMaxBytes {
				t.Fatalf("effective budget %d exceeds the ceiling %d", got, maxMaxBytes)
			}
		})
	}

	if maxMaxBytes != 1_000_000 {
		t.Fatalf("maxMaxBytes = %d, want 1000000", maxMaxBytes)
	}
}

func TestGetMultipleDocumentsCapsOversizedMaxBytes(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{Folders: []config.FolderConfig{{Path: "/repo"}}}
	svc := NewDocumentService(store, cfg)

	// Roughly 1.5 MB of chunk text across 30 files, more than the ceiling.
	body := strings.Repeat("x", 50_000)
	for i := range 30 {
		fp := fmt.Sprintf("/repo/file-%02d.go", i)
		store.addChunks("/repo", fp, []domain.Chunk{{FilePath: fp, LineNum: 1, EndLine: 1, Text: body}})
	}

	paths := make([]string, 30)
	for i := range paths {
		paths[i] = fmt.Sprintf("/repo/file-%02d.go", i)
	}

	results, truncated, err := svc.GetMultipleDocuments(strings.Join(paths, ","), 50_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation at the 1000000-byte ceiling, not the requested 50000000")
	}

	total := 0
	for _, r := range results {
		for _, c := range r.Chunks {
			total += len(c.Text)
		}
	}
	if total > maxMaxBytes {
		t.Fatalf("returned %d bytes of chunk text, above the %d ceiling", total, maxMaxBytes)
	}
	if total == 0 {
		t.Fatal("expected some content within the ceiling")
	}
}

func TestResolveSource(t *testing.T) {
	svc := &DocumentService{
		cfg: &config.Config{
			Folders: []config.FolderConfig{
				{Path: "/repo"},
				{Path: "/other/project"},
			},
		},
	}

	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{"prefix match", "/repo/src/main.go", "/repo"},
		{"exact match", "/repo", "/repo"},
		{"second folder", "/other/project/file.go", "/other/project"},
		{"no match", "/unknown/file.go", ""},
		{"partial prefix mismatch", "/repo-extra/file.go", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.resolveSource(tt.filePath)
			if got != tt.want {
				t.Errorf("resolveSource(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestIsGlob(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"*.go", true},
		{"file?.txt", true},
		{"[abc].go", true},
		{"/plain/path.go", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if got := isGlob(tt.pattern); got != tt.want {
				t.Errorf("isGlob(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}
