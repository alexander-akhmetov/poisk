package index

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/chunk"
	ignore "github.com/sabhiram/go-gitignore"
)

// docExtensions are always included for markdown/text content.
var docExtensions = []string{".md", ".markdown", ".txt", ".org"}

func buildExtensionSet() map[string]bool {
	extSet := make(map[string]bool)

	for _, ext := range chunk.SupportedExtensions(chunk.SupportedLanguages()) {
		extSet["."+ext] = true
	}

	for _, ext := range docExtensions {
		extSet[ext] = true
	}

	return extSet
}

// gitignoreStack tracks nested .gitignore files during directory traversal.
// Each entry maps a directory path to its compiled gitignore matcher.
type gitignoreStack struct {
	entries []gitignoreEntry
}

type gitignoreEntry struct {
	dir string
	gi  *ignore.GitIgnore
}

func newGitignoreStack() *gitignoreStack {
	return &gitignoreStack{}
}

func (s *gitignoreStack) load(dir string) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	gi, err := ignore.CompileIgnoreFile(gitignorePath)
	if err != nil {
		return
	}
	slog.Info("loaded .gitignore", "path", gitignorePath)
	s.entries = append(s.entries, gitignoreEntry{dir: dir, gi: gi})
}

func (s *gitignoreStack) matchesPath(path string, isDir bool) bool {
	for _, e := range s.entries {
		rel, err := filepath.Rel(e.dir, path)
		if err != nil || rel == "." {
			continue
		}
		// Only check if path is under this gitignore's directory.
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		checkPath := rel
		if isDir {
			checkPath = rel + "/"
		}
		if e.gi.MatchesPath(checkPath) {
			return true
		}
	}
	return false
}

func matchesExcludePattern(name string, excludePatterns []string) bool {
	for _, p := range excludePatterns {
		ok, err := filepath.Match(p, name)
		if err != nil {
			slog.Warn("bad exclude pattern", "pattern", p, "error", err)
			continue
		}
		if ok {
			return true
		}
	}
	return false
}

func scanFolder(root string, excludePatterns, includePatterns []string, maxSizeKB int) ([]string, error) {
	// Resolve symlinks so WalkDir can traverse the real directory.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = resolved

	extSet := buildExtensionSet()
	gis := newGitignoreStack()
	gis.load(root)

	maxBytes := int64(maxSizeKB) * 1024
	var files []string

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("scan error", "path", path, "error", err)
			return nil
		}

		name := d.Name()

		if d.IsDir() {
			if matchesExcludePattern(name, excludePatterns) {
				return filepath.SkipDir
			}
			if gis.matchesPath(path, true) {
				return filepath.SkipDir
			}
			if path != root {
				gis.load(path)
			}
			return nil
		}

		if gis.matchesPath(path, false) {
			return nil
		}

		if len(includePatterns) > 0 {
			matched := false
			for _, p := range includePatterns {
				if ok, _ := filepath.Match(p, name); ok {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if !extSet[ext] {
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			slog.Warn("stat failed", "path", path, "error", err)
			return nil
		}
		if info.Size() > maxBytes {
			return nil
		}

		files = append(files, path)
		return nil
	})

	slog.Info("scan complete", "folder", root, "files_found", len(files))
	return files, err
}
