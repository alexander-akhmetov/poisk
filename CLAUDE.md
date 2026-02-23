# poisk

Hybrid search with MCP interface. Go binary that indexes source code and markdown files with embeddings into SQLite and exposes hybrid semantic+keyword search via MCP.

## Build & Test

```bash
make build    # builds with -tags fts5
make test     # runs all tests
```

The `-tags fts5` build tag is required to enable FTS5 support in go-sqlite3.

## Architecture

- `cmd/poisk/` — CLI entry point with serve/index/search/status subcommands
- `internal/config/` — TOML config at `~/.config/poisk/config.toml`
- `internal/store/` — SQLite with sqlite-vec (vec0) + FTS5
- `internal/embed/` — OpenAI-compatible embedding client
- `internal/chunk/` — Markdown paragraph + source code fixed-window chunkers
- `internal/treesitter/commonlisp/` — CGo binding for tree-sitter-commonlisp grammar
- `internal/index/` — Incremental file indexer with mtime tracking
- `internal/search/` — Hybrid vec0 KNN + FTS5 BM25 with weighted merge
- `internal/mcp/` — MCP server (search/reindex tools + index-status resource)

## Key Decisions

- sqlite-vec via `github.com/asg017/sqlite-vec-go-bindings/cgo` with `Auto()` global registration
- No brute-force cosine fallback — vec0 only for vector search
- `database/sql` connection pool handles concurrency; indexer has `sync.Mutex`
- Logging to stderr via `log/slog` (stdout reserved for MCP stdio)
