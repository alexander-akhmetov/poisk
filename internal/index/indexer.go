package index

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/akhmetov/poisk/internal/chunk"
	"github.com/akhmetov/poisk/internal/config"
	"github.com/akhmetov/poisk/internal/embed"
	"github.com/akhmetov/poisk/internal/store"
)

type Indexer struct {
	store  *store.Store
	client *embed.Client
	cfg    *config.Config
	mu     sync.Mutex
}

type FolderStats struct {
	Folder         string
	FilesProcessed int
	FilesSkipped   int
	ChunksCreated  int
	Errors         int
}

func NewIndexer(s *store.Store, c *embed.Client, cfg *config.Config) *Indexer {
	return &Indexer{store: s, client: c, cfg: cfg}
}

func (ix *Indexer) IndexAll(ctx context.Context) ([]FolderStats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	var allStats []FolderStats
	for _, f := range ix.cfg.Folders {
		stats, err := ix.indexFolder(ctx, f.Path)
		if err != nil {
			return allStats, err
		}
		allStats = append(allStats, stats)
	}
	return allStats, nil
}

func (ix *Indexer) IndexFolder(ctx context.Context, folder string) (FolderStats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.indexFolder(ctx, folder)
}

func (ix *Indexer) indexFolder(ctx context.Context, folder string) (FolderStats, error) {
	stats := FolderStats{Folder: folder}

	// Check model change
	changed, err := ix.store.ModelChanged(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions)
	if err != nil {
		return stats, err
	}
	if changed {
		slog.Info("model changed, rebuilding", "folder", folder)
		if err := ix.store.ClearSource(folder); err != nil {
			return stats, fmt.Errorf("clear source %s: %w", folder, err)
		}
	}

	// Scan files
	files, err := scanFolder(folder, ix.cfg.Index.Extensions, ix.cfg.Index.ExcludePatterns, ix.cfg.Index.MaxFileSizeKB)
	if err != nil {
		return stats, err
	}

	// Get tracked files for mtime comparison
	tracked, err := ix.store.TrackedFiles(folder)
	if err != nil {
		return stats, err
	}

	// Prune deleted files
	currentSet := make(map[string]bool, len(files))
	for _, f := range files {
		currentSet[f] = true
	}
	for fp := range tracked {
		if !currentSet[fp] {
			if err := ix.store.DeleteFile(folder, fp); err != nil {
				slog.Error("delete stale file failed", "file", fp, "error", err)
				stats.Errors++
			}
		}
	}

	// Process changed files
	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		info, err := os.Stat(filePath)
		if err != nil {
			stats.Errors++
			continue
		}
		mtime := info.ModTime().Unix()

		// Skip if unchanged
		if oldMtime, ok := tracked[filePath]; ok && oldMtime == mtime && !changed {
			stats.FilesSkipped++
			continue
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			stats.Errors++
			continue
		}

		chunks := chunk.File(filePath, string(content))
		if len(chunks) == 0 {
			if err := ix.store.SetFileMtime(folder, filePath, mtime); err != nil {
				slog.Error("set mtime failed", "file", filePath, "error", err)
				stats.Errors++
			} else {
				stats.FilesSkipped++
			}
			continue
		}

		// Batch embed
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}

		// Process in batches — only commit if ALL batches succeed
		var entries []store.Entry
		batchSize := ix.cfg.Embedding.BatchSize
		embedFailed := false
		for i := 0; i < len(texts); i += batchSize {
			end := min(i+batchSize, len(texts))

			embeddings, err := ix.client.EmbedBatch(ctx, texts[i:end])
			if err != nil {
				slog.Error("embedding failed", "file", filePath, "error", err)
				stats.Errors++
				embedFailed = true
				break
			}

			for j, emb := range embeddings {
				entries = append(entries, store.Entry{
					Source:    folder,
					FilePath:  filePath,
					LineNum:   chunks[i+j].LineNum,
					Text:      chunks[i+j].Text,
					Embedding: emb,
					Folder:    folder,
				})
			}
		}

		if embedFailed {
			continue
		}

		if len(entries) > 0 {
			if err := ix.store.InsertEntries(folder, filePath, entries); err != nil {
				slog.Error("insert failed", "file", filePath, "error", err)
				stats.Errors++
				continue
			}
			if err := ix.store.SetFileMtime(folder, filePath, mtime); err != nil {
				slog.Error("set mtime failed", "file", filePath, "error", err)
				stats.Errors++
				continue
			}
			stats.FilesProcessed++
			stats.ChunksCreated += len(entries)
		}
	}

	// Update meta
	if err := ix.store.UpdateMeta(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions); err != nil {
		return stats, fmt.Errorf("update meta for %s: %w", folder, err)
	}

	return stats, nil
}
