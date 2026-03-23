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

func TestScanFolderNestedGitignore(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	// Root .gitignore ignores *.log
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create nested structure: src/ has its own .gitignore ignoring generated/
	for _, d := range []string{"src", "src/generated", "src/core", "lib"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "src", ".gitignore"), []byte("generated/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, f := range []struct{ path, content string }{
		{filepath.Join("src", "core", "main.go"), "package core"},
		{filepath.Join("src", "generated", "gen.go"), "package gen"},
		{filepath.Join("lib", "util.go"), "package lib"},
		{filepath.Join("lib", "debug.log"), "log"},
		{"app.go", "package main"},
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

	// Included
	if !got[filepath.Join("src", "core", "main.go")] {
		t.Error("expected src/core/main.go to be included")
	}
	if !got[filepath.Join("lib", "util.go")] {
		t.Error("expected lib/util.go to be included")
	}
	if !got["app.go"] {
		t.Error("expected app.go to be included")
	}

	// Excluded by nested src/.gitignore
	if got[filepath.Join("src", "generated", "gen.go")] {
		t.Error("expected src/generated/gen.go to be excluded by nested .gitignore")
	}

	// Excluded by root .gitignore
	if got[filepath.Join("lib", "debug.log")] {
		t.Error("expected lib/debug.log to be excluded by root .gitignore")
	}
}

func TestScanFolderNestedGitignoreSiblingIsolation(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	// dirA/.gitignore ignores "secret/" — this must NOT affect dirB
	for _, d := range []string{"dirA", "dirA/secret", "dirB", "dirB/secret"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "dirA", ".gitignore"), []byte("secret/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ path, content string }{
		{filepath.Join("dirA", "secret", "hidden.go"), "package hidden"},
		{filepath.Join("dirB", "secret", "visible.go"), "package visible"},
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

	if got[filepath.Join("dirA", "secret", "hidden.go")] {
		t.Error("expected dirA/secret/hidden.go to be excluded by dirA/.gitignore")
	}
	if !got[filepath.Join("dirB", "secret", "visible.go")] {
		t.Error("expected dirB/secret/visible.go to be included (dirA/.gitignore should not affect dirB)")
	}
}

func TestScanFolderExcludeGlobPatterns(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	for _, d := range []string{"src", "build_output", "test_data", "docs"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct{ path, content string }{
		{filepath.Join("src", "main.go"), "package main"},
		{filepath.Join("build_output", "out.go"), "package out"},
		{filepath.Join("test_data", "fixture.go"), "package fixture"},
		{filepath.Join("docs", "readme.md"), "# docs"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.path), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Glob patterns: exclude dirs starting with "build_" or "test_"
	files, err := scanFolder(dir, []string{"build_*", "test_*"}, nil, 512)
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
	if !got[filepath.Join("docs", "readme.md")] {
		t.Error("expected docs/readme.md to be included")
	}
	if got[filepath.Join("build_output", "out.go")] {
		t.Error("expected build_output/out.go to be excluded by glob pattern")
	}
	if got[filepath.Join("test_data", "fixture.go")] {
		t.Error("expected test_data/fixture.go to be excluded by glob pattern")
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
