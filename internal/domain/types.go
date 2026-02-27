// Package domain defines core entities and value types shared across the
// application. These types have no dependencies on infrastructure packages
// (database, HTTP clients, etc.).
package domain

type Chunk struct {
	Source   string
	FilePath string
	LineNum  int
	EndLine  int
	Text     string
	Folder   string
	Language string
	Kind     string
	Symbol   string
}

type ChunkWithEmbedding struct {
	Chunk
	Embedding []float32
}

type SearchResult struct {
	FilePath string
	LineNum  int
	EndLine  int
	Text     string
	Score    float64
	Folder   string
	Language string
	Kind     string
	Symbol   string
	Context  []string
}

// ModelChange describes the result of a model change check.
type ModelChange struct {
	Changed  bool
	OldModel string
	OldDims  int
}

type FolderStats struct {
	Folder                 string
	FilesProcessed         int
	FilesSkipped           int
	ChunksCreated          int
	Errors                 int
	FilesSkippedParseError int
}

type FolderStatus struct {
	Path        string
	Description string
	Files       int
	Chunks      int
	Context     map[string]string
}

type IndexStatus struct {
	VecAvailable bool
	FTSAvailable bool
	Folders      []FolderStatus
}
