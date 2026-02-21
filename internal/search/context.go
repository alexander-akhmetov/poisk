package search

import (
	"path/filepath"
	"sort"
	"strings"
)

// resolveContext finds matching context descriptions for a file path
// by checking prefix matches against the context map keys.
// Returns descriptions ordered from general (shorter prefix) to specific (longer prefix).
func ResolveContext(filePath, folderPath string, contextMap map[string]string) []string {
	if len(contextMap) == 0 {
		return nil
	}

	// Make file path relative to the folder
	relPath := filePath
	if strings.HasPrefix(filePath, folderPath) {
		rel, err := filepath.Rel(folderPath, filePath)
		if err == nil {
			relPath = rel
		}
	}
	relPath = filepath.ToSlash(relPath)

	type match struct {
		prefix string
		desc   string
	}

	var matches []match
	for prefix, desc := range contextMap {
		prefix = filepath.ToSlash(prefix)
		// Root-level context matches everything
		if prefix == "" || prefix == "." {
			matches = append(matches, match{prefix: "", desc: desc})
			continue
		}
		if relPath == prefix || strings.HasPrefix(relPath, prefix+"/") {
			matches = append(matches, match{prefix: prefix, desc: desc})
		}
	}

	if len(matches) == 0 {
		return nil
	}

	// Sort by prefix length (general to specific)
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i].prefix) < len(matches[j].prefix)
	})

	descriptions := make([]string, len(matches))
	for i, m := range matches {
		descriptions[i] = m.desc
	}
	return descriptions
}
