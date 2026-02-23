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
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.MkdirAll(filepath.Join(dir, "build"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "build", "out.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "debug.log"), []byte("log"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0644)

	files, err := scanFolder(dir, []string{".git"}, 512)
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

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	files, err := scanFolder(dir, []string{".git"}, 512)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}
