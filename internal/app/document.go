package app

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/domain"
	"github.com/akhmetov/poisk/internal/ports"
	"github.com/akhmetov/poisk/internal/search"
)

type DocumentService struct {
	store ports.ChunkStore
	cfg   *config.Config
}

func NewDocumentService(store ports.ChunkStore, cfg *config.Config) *DocumentService {
	return &DocumentService{store: store, cfg: cfg}
}

func (d *DocumentService) GetDocument(filePath string, startLine, endLine int) ([]domain.Chunk, []string, error) {
	source := d.resolveSource(filePath)
	if source == "" {
		return nil, nil, fmt.Errorf("file %q not under any configured folder", filePath)
	}

	chunks, err := d.store.GetChunksByPath(source, filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("get chunks: %w", err)
	}

	if startLine > 0 || endLine > 0 {
		var filtered []domain.Chunk
		for _, c := range chunks {
			chunkEnd := c.EndLine
			if chunkEnd <= 0 {
				chunkEnd = c.LineNum
			}
			if startLine > 0 && chunkEnd < startLine {
				continue
			}
			if endLine > 0 && c.LineNum > endLine {
				continue
			}
			filtered = append(filtered, c)
		}
		chunks = filtered
	}

	var context []string
	if fc := d.findFolderConfig(source); fc != nil && len(fc.Context) > 0 {
		context = search.ResolveContext(filePath, fc.Path, fc.Context)
	}

	return chunks, context, nil
}

func (d *DocumentService) GetMultipleDocuments(pathsCSV string, maxBytes int) ([]DocumentResult, bool, error) {
	if maxBytes <= 0 {
		maxBytes = 100_000
	}

	patterns := strings.Split(pathsCSV, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}

	var filePaths []string
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		if isGlob(pat) {
			for _, f := range d.cfg.Folders {
				tracked, err := d.store.TrackedFilePaths(f.Path)
				if err != nil {
					slog.Warn("multi_get: failed to list tracked paths", "source", f.Path, "error", err)
					continue
				}
				for _, tp := range tracked {
					matched, _ := filepath.Match(pat, tp)
					if !matched {
						matched, _ = filepath.Match(pat, filepath.Base(tp))
					}
					if matched {
						filePaths = append(filePaths, tp)
					}
				}
			}
		} else {
			filePaths = append(filePaths, pat)
		}
	}

	seen := make(map[string]bool, len(filePaths))
	deduped := filePaths[:0]
	for _, fp := range filePaths {
		if !seen[fp] {
			seen[fp] = true
			deduped = append(deduped, fp)
		}
	}
	filePaths = deduped

	if len(filePaths) == 0 {
		return nil, false, nil
	}

	var results []DocumentResult
	totalBytes := 0
	truncated := false

	for _, fp := range filePaths {
		if truncated {
			break
		}
		source := d.resolveSource(fp)
		if source == "" {
			continue
		}

		chunks, err := d.store.GetChunksByPath(source, fp)
		if err != nil {
			slog.Warn("multi_get: failed to get chunks", "file", fp, "error", err)
			continue
		}
		if len(chunks) == 0 {
			continue
		}

		var context []string
		if fc := d.findFolderConfig(source); fc != nil && len(fc.Context) > 0 {
			context = search.ResolveContext(fp, fc.Path, fc.Context)
		}

		headerSize := len("=== ") + len(fp) + len(" ===\n")
		if len(context) > 0 {
			headerSize += len("Context: ") + len(strings.Join(context, " > ")) + 1
		}
		if totalBytes+headerSize > maxBytes {
			truncated = true
			break
		}

		chunkBytes := 0
		var includedChunks []domain.Chunk
		for _, c := range chunks {
			loc := fmt.Sprintf("%s:%d", c.FilePath, c.LineNum)
			if c.EndLine > 0 && c.EndLine != c.LineNum {
				loc = fmt.Sprintf("%s:%d-%d", c.FilePath, c.LineNum, c.EndLine)
			}
			meta := ""
			if c.Symbol != "" {
				meta = fmt.Sprintf(" [%s]", c.Symbol)
			}
			chunkSize := len(loc) + len(meta) + 1 + len(c.Text) + 2 // loc+meta\ntext\n\n
			if totalBytes+headerSize+chunkBytes+chunkSize > maxBytes {
				truncated = true
				break
			}
			chunkBytes += chunkSize
			includedChunks = append(includedChunks, c)
		}

		if len(includedChunks) == 0 {
			truncated = true
			break
		}

		totalBytes += headerSize + chunkBytes
		results = append(results, DocumentResult{
			FilePath: fp,
			Chunks:   includedChunks,
			Context:  context,
		})
	}

	return results, truncated, nil
}

type DocumentResult struct {
	FilePath string
	Chunks   []domain.Chunk
	Context  []string
}

func (d *DocumentService) resolveSource(filePath string) string {
	for _, f := range d.cfg.Folders {
		if filePath == f.Path || strings.HasPrefix(filePath, f.Path+"/") {
			return f.Path
		}
	}
	return ""
}

func (d *DocumentService) findFolderConfig(source string) *config.FolderConfig {
	for i := range d.cfg.Folders {
		if d.cfg.Folders[i].Path == source {
			return &d.cfg.Folders[i]
		}
	}
	return nil
}

func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
