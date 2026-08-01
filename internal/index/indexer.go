package index

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/chunk"
	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/store"
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

	// Prune sources no longer in config
	dbSources, err := ix.store.AllSources()
	if err != nil {
		return allStats, fmt.Errorf("list sources: %w", err)
	}
	configured := make(map[string]bool, len(ix.cfg.Folders))
	for _, f := range ix.cfg.Folders {
		configured[f.Path] = true
	}
	for _, src := range dbSources {
		if !configured[src] {
			slog.Info("pruning removed folder", "source", src)
			if err := ix.store.ClearSource(src); err != nil {
				return allStats, fmt.Errorf("prune source %s: %w", src, err)
			}
		}
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
	mc, err := ix.store.ModelChanged(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions, ix.cfg.Embedding.Quantization)
	if err != nil {
		return stats, err
	}
	if mc.Changed {
		slog.Info("model changed, rebuilding",
			"folder", folder,
			"old_model", mc.OldModel, "old_dims", mc.OldDims, "old_quantization", mc.OldQuantization,
			"new_model", ix.cfg.Embedding.Model, "new_dims", ix.cfg.Embedding.Dimensions, "new_quantization", ix.cfg.Embedding.Quantization)
		if err := ix.store.ClearSource(folder); err != nil {
			return stats, fmt.Errorf("clear source %s: %w", folder, err)
		}
		// Write meta early so interrupted re-indexing resumes incrementally
		if err := ix.store.UpdateMeta(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions, ix.cfg.Embedding.Quantization); err != nil {
			return stats, fmt.Errorf("update meta for %s: %w", folder, err)
		}
	}

	// Resolve per-folder overrides
	excludePatterns := ix.cfg.Index.ExcludePatterns
	includePatterns := ix.cfg.Index.IncludePatterns
	maxFileSizeKB := ix.cfg.Index.MaxFileSizeKB
	for i := range ix.cfg.Folders {
		if ix.cfg.Folders[i].Path == folder {
			excludePatterns = ix.cfg.Folders[i].EffectiveExcludePatterns(ix.cfg.Index.ExcludePatterns)
			includePatterns = ix.cfg.Folders[i].EffectiveIncludePatterns(ix.cfg.Index.IncludePatterns)
			maxFileSizeKB = ix.cfg.Folders[i].EffectiveMaxFileSizeKB(ix.cfg.Index.MaxFileSizeKB)
			break
		}
	}

	// Scan files
	files, err := scanFolder(folder, excludePatterns, includePatterns, maxFileSizeKB)
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

	if err := ix.store.SetIndexingProgress(folder, len(files), 0, time.Now().Unix()); err != nil {
		slog.Error("set indexing progress failed", "folder", folder, "error", err)
	}

	// Process changed files. Files are chunked one at a time but embedded in
	// groups, because the average file produces far fewer chunks than
	// BatchSize and one HTTP request per file dominates indexing wall clock.
	batchSize := ix.cfg.Embedding.BatchSize
	var pending []pendingFile
	pendingChunks := 0

	flush := func(processed int) {
		if len(pending) == 0 {
			return
		}
		ix.embedGroup(ctx, folder, pending, &stats)
		pending = nil
		pendingChunks = 0
		if err := ix.store.UpdateIndexingProcessed(folder, processed); err != nil {
			slog.Error("update indexing progress failed", "folder", folder, "error", err)
		}
	}

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

		if oldMtime, ok := tracked[filePath]; ok && oldMtime == mtime && !mc.Changed {
			stats.FilesSkipped++
			continue
		}

		slog.Info("processing file", "file", filePath, "progress", fmt.Sprintf("%d/%d", i+1, len(files)))
		chunks, ok := ix.prepareFile(folder, filePath, mtime, &stats)
		if !ok || len(chunks) == 0 {
			continue
		}

		// A file with more chunks than BatchSize keeps the multi-request path
		// and is never grouped: its chunks must still reach InsertEntries in
		// one call.
		if len(chunks) > batchSize {
			flush(i)
			ix.embedGroup(ctx, folder, []pendingFile{{path: filePath, mtime: mtime, chunks: chunks}}, &stats)
			if err := ix.store.UpdateIndexingProcessed(folder, i+1); err != nil {
				slog.Error("update indexing progress failed", "folder", folder, "error", err)
			}
			continue
		}

		if pendingChunks+len(chunks) > batchSize {
			flush(i)
		}
		pending = append(pending, pendingFile{path: filePath, mtime: mtime, chunks: chunks})
		pendingChunks += len(chunks)
	}
	flush(len(files))
	// The loop's cancellation check sits ahead of the last group, so report a
	// run cancelled during that group instead of exiting as if it finished.
	if err := ctx.Err(); err != nil {
		return stats, err
	}

	if err := ix.store.ClearIndexingProgress(folder); err != nil {
		slog.Error("clear indexing progress failed", "folder", folder, "error", err)
	}

	slog.Info("folder done", "folder", folder, "processed", stats.FilesProcessed, "skipped", stats.FilesSkipped, "chunks", stats.ChunksCreated, "errors", stats.Errors)

	// Update meta
	if err := ix.store.UpdateMeta(folder, ix.cfg.Embedding.Model, ix.cfg.Embedding.Dimensions, ix.cfg.Embedding.Quantization); err != nil {
		return stats, fmt.Errorf("update meta for %s: %w", folder, err)
	}

	return stats, nil
}

// pendingFile is a chunked file waiting to be embedded with its neighbours.
type pendingFile struct {
	path   string
	mtime  int64
	chunks []chunk.Chunk
}

// prepareFile reads and chunks one file. It reports whether the file is
// eligible for embedding; a file that produced no chunks is finished here, on
// the path that clears its stale entries and advances its mtime.
func (ix *Indexer) prepareFile(folder, filePath string, mtime int64, stats *FolderStats) ([]chunk.Chunk, bool) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		ix.clearStaleEntries(folder, filePath, "read_failed", err)
		stats.Errors++
		return nil, false
	}

	chunks, chunkErr := chunk.File(filePath, string(content))
	if chunkErr != nil {
		ix.clearStaleEntries(folder, filePath, "chunk_parse_failed", chunkErr)
		slog.Warn("chunk parse error", "file", filePath, "error", chunkErr)
		stats.FilesSkippedParseError++
		return nil, false
	}
	if len(chunks) == 0 {
		if err := ix.store.InsertEntries(folder, filePath, nil); err != nil {
			slog.Error("clear entries failed", "file", filePath, "error", err)
			stats.Errors++
			return nil, false
		}
		if err := ix.store.SetFileMtime(folder, filePath, mtime); err != nil {
			slog.Error("set mtime failed", "file", filePath, "error", err)
			stats.Errors++
		} else {
			stats.FilesSkipped++
		}
		return nil, false
	}

	return chunks, true
}

// embedGroup embeds every chunk in the group, then writes each file with one
// InsertEntries call followed by one SetFileMtime. A failed embedding request
// covers the whole group, so every file in it has its stale data cleared, keeps
// its old mtime, and counts one error. A cancelled run leaves the group's
// indexed data alone: the request says nothing about those files, and the
// mtimes stay old, so the next run re-indexes them.
func (ix *Indexer) embedGroup(ctx context.Context, folder string, group []pendingFile, stats *FolderStats) {
	texts := make([]string, 0, len(group))
	for _, pf := range group {
		for _, c := range pf.chunks {
			texts = append(texts, c.Text)
		}
	}

	embeddings, err := ix.embedTexts(ctx, texts, group[0].path)
	if err != nil {
		if ctx.Err() != nil {
			slog.Warn("embedding cancelled", "files", len(group), "error", err)
			stats.Errors += len(group)
			return
		}
		for _, pf := range group {
			ix.clearStaleEntries(folder, pf.path, "embedding_failed", err)
		}
		slog.Error("embedding failed", "files", len(group), "error", err)
		stats.Errors += len(group)
		return
	}

	offset := 0
	for _, pf := range group {
		entries := make([]store.Entry, len(pf.chunks))
		for i, c := range pf.chunks {
			entries[i] = store.Entry{
				Source:    folder,
				FilePath:  pf.path,
				LineNum:   c.StartLine,
				EndLine:   c.EndLine,
				Text:      c.Text,
				Embedding: embeddings[offset+i],
				Folder:    folder,
				Language:  c.Language,
				Kind:      c.Kind,
				Symbol:    c.Symbol,
			}
		}
		offset += len(pf.chunks)

		if err := ix.store.InsertEntries(folder, pf.path, entries); err != nil {
			slog.Error("insert failed", "file", pf.path, "error", err)
			stats.Errors++
			continue
		}
		if err := ix.store.SetFileMtime(folder, pf.path, pf.mtime); err != nil {
			slog.Error("set mtime failed", "file", pf.path, "error", err)
			stats.Errors++
			continue
		}
		stats.FilesProcessed++
		stats.ChunksCreated += len(entries)
	}
}

// embedTexts sends texts to the embedding API, splitting into BatchSize
// requests. A group only exceeds BatchSize when it holds a single oversized
// file, so this stays a one-request call for grouped files. logPath names that
// oversized file in the per-batch progress log.
func (ix *Indexer) embedTexts(ctx context.Context, texts []string, logPath string) ([][]float32, error) {
	batchSize := ix.cfg.Embedding.BatchSize
	totalBatches := (len(texts) + batchSize - 1) / batchSize

	embeddings := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		end := min(i+batchSize, len(texts))
		if totalBatches > 1 {
			slog.Info("embedding batch", "file", logPath, "batch", fmt.Sprintf("%d/%d", i/batchSize+1, totalBatches))
		}

		batch, err := ix.client.EmbedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, batch...)
	}

	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d for %d chunks", len(embeddings), len(texts))
	}
	return embeddings, nil
}

func (ix *Indexer) clearStaleEntries(source, filePath, reason string, cause error) {
	slog.Warn("clearing stale indexed data after indexing failure", "source", source, "file", filePath, "reason", reason, "error", cause)
	if err := ix.store.InsertEntries(source, filePath, nil); err != nil {
		slog.Error("failed to clear stale indexed data", "source", source, "file", filePath, "reason", reason, "error", err)
	}
}
