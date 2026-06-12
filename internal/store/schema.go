package store

import "strconv"

const schemaVersion = 6

// Quantization modes for vec0 vector storage.
const (
	QuantizationInt8    = "int8"
	QuantizationFloat32 = "float32"
)

const schemaVersionDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
`

const schemaSQL = `
CREATE TABLE IF NOT EXISTS embedding_files (
    source    TEXT NOT NULL,
    file_path TEXT NOT NULL,
    mtime     INTEGER NOT NULL,
    PRIMARY KEY (source, file_path)
);

CREATE TABLE IF NOT EXISTS embeddings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source     TEXT NOT NULL,
    file_path  TEXT NOT NULL,
    line_num   INTEGER NOT NULL,
    chunk_text TEXT NOT NULL,
    folder     TEXT,
    end_line   INTEGER NOT NULL DEFAULT 0,
    language   TEXT NOT NULL DEFAULT '',
    chunk_kind TEXT NOT NULL DEFAULT '',
    symbol     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_emb_source_file ON embeddings(source, file_path);
CREATE INDEX IF NOT EXISTS idx_emb_meta ON embeddings(language, chunk_kind, symbol);

CREATE TABLE IF NOT EXISTS embedding_meta (
    source       TEXT PRIMARY KEY NOT NULL,
    model        TEXT NOT NULL,
    dimensions   INTEGER NOT NULL,
    quantization TEXT NOT NULL
);
`

const indexingProgressDDL = `CREATE TABLE IF NOT EXISTS indexing_progress (
    folder     TEXT PRIMARY KEY NOT NULL,
    total      INTEGER NOT NULL,
    processed  INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER NOT NULL
)`

func vec0DDL(dims int, quantization string) string {
	elem := "float"
	if quantization == QuantizationInt8 {
		elem = "int8"
	}
	return `CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(embedding ` + elem + `[` + strconv.Itoa(dims) + `] distance_metric=cosine)`
}

// chunks_fts is an external-content table: chunk text lives only in
// embeddings, the FTS table keeps just the inverted index. Writers must keep
// the index in sync manually (see deleteFTSRows and the insert in
// InsertEntries).
const fts5DDL = `CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(chunk_text, source UNINDEXED, file_path UNINDEXED, line_num UNINDEXED, folder UNINDEXED, end_line UNINDEXED, language, chunk_kind, symbol, content='embeddings', content_rowid='id')`
