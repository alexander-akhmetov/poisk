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
	Folder                 string
	FilesProcessed         int
	FilesSkipped           int
	ChunksCreated          int
	Errors                 int
	FilesSkippedParseError int
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
		// Write meta early so interrupted re-indexing resumes incrementally
		if err := ix.store.UpdateMeta(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions); err != nil {
			return stats, fmt.Errorf("update meta for %s: %w", folder, err)
		}
	}

	// Scan files
	files, err := scanFolder(folder, ix.cfg.Index.ExcludePatterns, ix.cfg.Index.MaxFileSizeKB)
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

	slog.Info("indexing folder", "folder", folder, "files", len(files))

	// Process changed files
	for i, filePath := range files {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		info, err := os.Stat(filePath)
		if err != nil {
			ix.clearStaleEntries(folder, filePath, "stat_failed", err)
			stats.Errors++
			continue
		}
		mtime := info.ModTime().UnixNano()

		if oldMtime, ok := tracked[filePath]; ok && oldMtime == mtime && !changed {
			stats.FilesSkipped++
			continue
		}

		slog.Info("processing file", "file", filePath, "progress", fmt.Sprintf("%d/%d", i+1, len(files)))
		ix.processFile(ctx, folder, filePath, mtime, &stats)
	}

	slog.Info("folder done", "folder", folder, "processed", stats.FilesProcessed, "skipped", stats.FilesSkipped, "chunks", stats.ChunksCreated, "errors", stats.Errors)

	// Update meta
	if err := ix.store.UpdateMeta(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions); err != nil {
		return stats, fmt.Errorf("update meta for %s: %w", folder, err)
	}

	return stats, nil
}

func (ix *Indexer) processFile(ctx context.Context, folder, filePath string, mtime int64, stats *FolderStats) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		ix.clearStaleEntries(folder, filePath, "read_failed", err)
		stats.Errors++
		return
	}

	chunks, chunkErr := chunk.File(filePath, string(content))
	if chunkErr != nil {
		ix.clearStaleEntries(folder, filePath, "chunk_parse_failed", chunkErr)
		slog.Warn("chunk parse error", "file", filePath, "error", chunkErr)
		stats.FilesSkippedParseError++
		return
	}
	if len(chunks) == 0 {
		if err := ix.store.InsertEntries(folder, filePath, nil); err != nil {
			slog.Error("clear entries failed", "file", filePath, "error", err)
			stats.Errors++
			return
		}
		if err := ix.store.SetFileMtime(folder, filePath, mtime); err != nil {
			slog.Error("set mtime failed", "file", filePath, "error", err)
			stats.Errors++
		} else {
			stats.FilesSkipped++
		}
		return
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}

	var entries []store.Entry
	batchSize := ix.cfg.Embedding.BatchSize
	totalBatches := (len(texts) + batchSize - 1) / batchSize
	for i := 0; i < len(texts); i += batchSize {
		end := min(i+batchSize, len(texts))
		batchNum := i/batchSize + 1
		if totalBatches > 1 {
			slog.Info("embedding batch", "file", filePath, "batch", fmt.Sprintf("%d/%d", batchNum, totalBatches))
		}

		embeddings, err := ix.client.EmbedBatch(ctx, texts[i:end])
		if err != nil {
			ix.clearStaleEntries(folder, filePath, "embedding_failed", err)
			slog.Error("embedding failed", "file", filePath, "error", err)
			stats.Errors++
			return
		}

		for j, emb := range embeddings {
			c := chunks[i+j]
			entries = append(entries, store.Entry{
				Source:    folder,
				FilePath:  filePath,
				LineNum:   c.StartLine,
				EndLine:   c.EndLine,
				Text:      c.Text,
				Embedding: emb,
				Folder:    folder,
				Language:  c.Language,
				Kind:      c.Kind,
				Symbol:    c.Symbol,
			})
		}
	}

	if len(entries) > 0 {
		if err := ix.store.InsertEntries(folder, filePath, entries); err != nil {
			slog.Error("insert failed", "file", filePath, "error", err)
			stats.Errors++
			return
		}
		if err := ix.store.SetFileMtime(folder, filePath, mtime); err != nil {
			slog.Error("set mtime failed", "file", filePath, "error", err)
			stats.Errors++
			return
		}
		stats.FilesProcessed++
		stats.ChunksCreated += len(entries)
	}
}

func (ix *Indexer) clearStaleEntries(source, filePath, reason string, cause error) {
	slog.Warn("clearing stale indexed data after indexing failure", "source", source, "file", filePath, "reason", reason, "error", cause)
	if err := ix.store.InsertEntries(source, filePath, nil); err != nil {
		slog.Error("failed to clear stale indexed data", "source", source, "file", filePath, "reason", reason, "error", err)
	}
}
