// Package ports defines interfaces for infrastructure adapters. App-layer
// services depend on these interfaces, not on concrete implementations.
package ports

import "github.com/alexander-akhmetov/poisk/internal/domain"

// ChunkStore persists and retrieves indexed chunks and file metadata.
type ChunkStore interface {
	InsertChunks(source, filePath string, chunks []domain.ChunkWithEmbedding) error
	GetChunksByPath(source, filePath string) ([]domain.Chunk, error)
	DeleteFile(source, filePath string) error
	ClearSource(source string) error
	Count(source string) (int, error)

	// File mtime tracking for incremental indexing.
	GetFileMtime(source, filePath string) (int64, bool, error)
	SetFileMtime(source, filePath string, mtime int64) error
	TrackedFiles(source string) (map[string]int64, error)
	TrackedFilePaths(source string) ([]string, error)
	TrackedFileCount(source string) (int, error)

	// Model metadata tracking (for change detection).
	ModelChanged(source, model string, dimensions int) (domain.ModelChange, error)
	UpdateMeta(source, model string, dimensions int) error
	AllSources() ([]string, error)

	// Index status.
	VecAvailable() bool
	FTSAvailable() bool
}
