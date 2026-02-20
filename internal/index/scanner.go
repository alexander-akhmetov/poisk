package index

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/akhmetov/poisk/internal/chunk"
)

// docExtensions are always included for markdown/text content.
var docExtensions = []string{".md", ".markdown", ".txt", ".org"}

func buildExtensionSet(languages, legacyExtensions []string) map[string]bool {
	extSet := make(map[string]bool)

	// Language-based extensions from tree-sitter
	if len(languages) > 0 {
		for _, ext := range chunk.SupportedExtensions(languages) {
			extSet["."+ext] = true
		}
	}

	// Legacy extensions (backward compat)
	for _, e := range legacyExtensions {
		extSet["."+e] = true
	}

	// Always include doc extensions
	for _, ext := range docExtensions {
		extSet[ext] = true
	}

	return extSet
}

func scanFolder(root string, languages, extensions []string, excludePatterns []string, maxSizeKB int) ([]string, error) {
	extSet := buildExtensionSet(languages, extensions)

	maxBytes := int64(maxSizeKB) * 1024
	var files []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("scan error", "path", path, "error", err)
			return nil
		}

		name := d.Name()

		if d.IsDir() {
			for _, p := range excludePatterns {
				if name == p || strings.Contains(path, string(filepath.Separator)+p+string(filepath.Separator)) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !extSet[ext] {
			return nil
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

	return files, err
}
