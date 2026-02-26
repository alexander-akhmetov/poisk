package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFolderRespectsGitignore(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	// Create .gitignore that ignores "build/" dir and "*.log" files
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\n*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create files: one visible, one in ignored dir, one with ignored extension
	for _, d := range []string{"src", "build"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct{ path, content string }{
		{filepath.Join("src", "main.go"), "package main"},
		{filepath.Join("build", "out.go"), "package main"},
		{"debug.log", "log"},
		{"README.md", "# readme"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.path), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := scanFolder(dir, []string{".git"}, nil, 512)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got[filepath.Join("src", "main.go")] {
		t.Error("expected src/main.go to be included")
	}
	if !got["README.md"] {
		t.Error("expected README.md to be included")
	}
	if got[filepath.Join("build", "out.go")] {
		t.Error("expected build/out.go to be excluded by .gitignore")
	}
	if got["debug.log"] {
		t.Error("expected debug.log to be excluded by .gitignore")
	}
}

func TestScanFolderNoGitignore(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := scanFolder(dir, []string{".git"}, nil, 512)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestScanFolderIncludePatterns(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	for _, f := range []struct{ name, content string }{
		{"main.go", "package main"},
		{"lib.py", "print('hello')"},
		{"README.md", "# readme"},
		{"notes.txt", "notes"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Only include Go files
	files, err := scanFolder(dir, nil, []string{"*.go"}, 512)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got["main.go"] {
		t.Error("expected main.go to be included")
	}
	if got["lib.py"] {
		t.Error("expected lib.py to be excluded by include_patterns")
	}
	if got["README.md"] {
		t.Error("expected README.md to be excluded by include_patterns")
	}
	if got["notes.txt"] {
		t.Error("expected notes.txt to be excluded by include_patterns")
	}
}

func TestScanFolderIncludePatternsMultiple(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	for _, f := range []struct{ name, content string }{
		{"main.go", "package main"},
		{"lib.py", "print('hello')"},
		{"README.md", "# readme"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := scanFolder(dir, nil, []string{"*.go", "*.md"}, 512)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got["main.go"] {
		t.Error("expected main.go to be included")
	}
	if !got["README.md"] {
		t.Error("expected README.md to be included")
	}
	if got["lib.py"] {
		t.Error("expected lib.py to be excluded by include_patterns")
	}
}

func TestScanFolderPerFolderExclude(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	for _, d := range []string{"src", "vendor"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct{ path, content string }{
		{filepath.Join("src", "main.go"), "package main"},
		{filepath.Join("vendor", "lib.go"), "package lib"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.path), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Exclude vendor at folder level
	files, err := scanFolder(dir, []string{"vendor"}, nil, 512)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got[filepath.Join("src", "main.go")] {
		t.Error("expected src/main.go to be included")
	}
	if got[filepath.Join("vendor", "lib.go")] {
		t.Error("expected vendor/lib.go to be excluded")
	}
}
