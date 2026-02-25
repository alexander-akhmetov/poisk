package store

import "github.com/alexander-akhmetov/poisk/internal/domain"

func (s *Store) InsertChunks(source, filePath string, chunks []domain.ChunkWithEmbedding) error {
	entries := make([]Entry, len(chunks))
	for i, c := range chunks {
		entries[i] = Entry{
			Source:    c.Source,
			FilePath:  c.FilePath,
			LineNum:   c.LineNum,
			EndLine:   c.EndLine,
			Text:      c.Text,
			Embedding: c.Embedding,
			Folder:    c.Folder,
			Language:  c.Language,
			Kind:      c.Kind,
			Symbol:    c.Symbol,
		}
	}
	return s.InsertEntries(source, filePath, entries)
}

func (s *Store) GetChunksByPath(source, filePath string) ([]domain.Chunk, error) {
	entries, err := s.GetEntriesByPath(source, filePath)
	if err != nil {
		return nil, err
	}
	chunks := make([]domain.Chunk, len(entries))
	for i, e := range entries {
		chunks[i] = domain.Chunk{
			Source:   e.Source,
			FilePath: e.FilePath,
			LineNum:  e.LineNum,
			EndLine:  e.EndLine,
			Text:     e.Text,
			Folder:   e.Folder,
			Language: e.Language,
			Kind:     e.Kind,
			Symbol:   e.Symbol,
		}
	}
	return chunks, nil
}
