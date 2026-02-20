package index

import (
	"os"
	"path/filepath"
	"strings"
)

func scanFolder(root string, extensions []string, excludePatterns []string, maxSizeKB int) ([]string, error) {
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet["."+e] = true
	}

	maxBytes := int64(maxSizeKB) * 1024
	var files []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
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
