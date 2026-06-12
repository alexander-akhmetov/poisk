package app

import (
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/domain"
)

// mockChunkStore is an in-memory implementation of ports.ChunkStore for testing.
type mockChunkStore struct {
	chunks  map[string][]domain.Chunk   // "source\x00filePath" → chunks
	tracked map[string]map[string]int64 // source → filePath → mtime

	vecAvailable     bool
	ftsAvailable     bool
	indexingProgress []domain.IndexingProgress

	errGetChunks    error
	errCount        error
	errClearSource  map[string]error // source → error
	errTrackedPaths error
	errTrackedCount error
}

func newMockStore() *mockChunkStore {
	return &mockChunkStore{
		chunks:  make(map[string][]domain.Chunk),
		tracked: make(map[string]map[string]int64),
	}
}

func (m *mockChunkStore) chunkKey(source, filePath string) string {
	return source + "\x00" + filePath
}

func (m *mockChunkStore) addChunks(source, filePath string, chunks []domain.Chunk) {
	m.chunks[m.chunkKey(source, filePath)] = chunks
	if m.tracked[source] == nil {
		m.tracked[source] = make(map[string]int64)
	}
	m.tracked[source][filePath] = 0
}

// --- ports.ChunkStore implementation ---

func (m *mockChunkStore) InsertChunks(_, _ string, _ []domain.ChunkWithEmbedding) error {
	return nil
}

func (m *mockChunkStore) GetChunksByPath(source, filePath string) ([]domain.Chunk, error) {
	if m.errGetChunks != nil {
		return nil, m.errGetChunks
	}
	return m.chunks[m.chunkKey(source, filePath)], nil
}

func (m *mockChunkStore) DeleteFile(_, _ string) error { return nil }

func (m *mockChunkStore) ClearSource(source string) error {
	if m.errClearSource != nil {
		if err, ok := m.errClearSource[source]; ok {
			return err
		}
	}
	for k := range m.chunks {
		if strings.HasPrefix(k, source+"\x00") {
			delete(m.chunks, k)
		}
	}
	delete(m.tracked, source)
	return nil
}

func (m *mockChunkStore) Count(source string) (int, error) {
	if m.errCount != nil {
		return 0, m.errCount
	}
	count := 0
	prefix := source + "\x00"
	for k, chunks := range m.chunks {
		if strings.HasPrefix(k, prefix) {
			count += len(chunks)
		}
	}
	return count, nil
}

func (m *mockChunkStore) GetFileMtime(source, filePath string) (int64, bool, error) {
	if t, ok := m.tracked[source]; ok {
		if mt, ok := t[filePath]; ok {
			return mt, true, nil
		}
	}
	return 0, false, nil
}

func (m *mockChunkStore) SetFileMtime(_, _ string, _ int64) error { return nil }

func (m *mockChunkStore) TrackedFiles(source string) (map[string]int64, error) {
	return m.tracked[source], nil
}

func (m *mockChunkStore) TrackedFilePaths(source string) ([]string, error) {
	if m.errTrackedPaths != nil {
		return nil, m.errTrackedPaths
	}
	t := m.tracked[source]
	paths := make([]string, 0, len(t))
	for p := range t {
		paths = append(paths, p)
	}
	return paths, nil
}

func (m *mockChunkStore) TrackedFileCount(source string) (int, error) {
	if m.errTrackedCount != nil {
		return 0, m.errTrackedCount
	}
	return len(m.tracked[source]), nil
}

func (m *mockChunkStore) ModelChanged(_, _ string, _ int, _ string) (domain.ModelChange, error) {
	return domain.ModelChange{}, nil
}
func (m *mockChunkStore) UpdateMeta(_, _ string, _ int, _ string) error { return nil }

func (m *mockChunkStore) AllSources() ([]string, error) {
	sources := make([]string, 0, len(m.tracked))
	for s := range m.tracked {
		sources = append(sources, s)
	}
	return sources, nil
}

func (m *mockChunkStore) VecAvailable() bool { return m.vecAvailable }
func (m *mockChunkStore) FTSAvailable() bool { return m.ftsAvailable }

func (m *mockChunkStore) GetIndexingProgress() ([]domain.IndexingProgress, error) {
	return m.indexingProgress, nil
}
