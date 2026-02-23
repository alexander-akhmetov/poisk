# poisk

Hybrid semantic + keyword search over local codebases via [MCP](https://modelcontextprotocol.io/).

Indexes source code and markdown files with embeddings into SQLite ([sqlite-vec](https://github.com/asg017/sqlite-vec) + FTS5) and exposes search through an MCP server that any compatible client (Claude Code, etc.) can use.

## Features

- **Hybrid search** — combines vector similarity (vec0 KNN) with keyword relevance (FTS5 BM25), merged via Reciprocal Rank Fusion (RRF)
- **Code-aware FTS** — tokenizer splits camelCase/snake_case, staged retrieval (strict AND → relaxed OR → prefix OR)
- **Tree-sitter chunking** — AST-based code chunking for Go, Python, Rust, JavaScript, TypeScript (with JSX/TSX support)
- **Markdown chunking** — heading-aware sections with breadcrumb paths, fence-aware splitting, large section token-budget splitting
- **Incremental indexing** — tracks file mtimes, only re-embeds changed files
- **Model change detection** — automatically rebuilds when embedding model or dimensions change
- **MCP interface** — `search` and `reindex` tools + `poisk://index-status` resource
- **Multiple folders** — index and search across multiple configured directories with per-folder filtering

## Install

Requires Go 1.21+ and CGO (for sqlite3 + sqlite-vec).

```bash
go install -tags fts5 github.com/akhmetov/poisk/cmd/poisk@latest
```

Or build from source:

```bash
git clone https://github.com/alexander-akhmetov/poisk.git
cd poisk
make build
```

## Configuration

Create `~/.config/poisk/config.toml`:

```toml
[embedding]
base_url = "http://localhost:11434/v1"  # Ollama, LM Studio, or any OpenAI-compatible API
api_key = ""
model = "nomic-embed-text"
dimensions = 768
batch_size = 50

[search]
rrf_k = 60                  # Reciprocal Rank Fusion constant (higher = more weight to top results)
similarity_threshold = 0.3   # Minimum cosine similarity for vector results
default_top_k = 20

[index]
languages = ["go", "python", "rust", "javascript", "typescript"]  # Tree-sitter supported languages
exclude_patterns = [".git", "node_modules", "vendor", "__pycache__", ".venv"]
max_file_size_kb = 512

[[folders]]
path = "/home/user/projects/myapp"
description = "My application"

[[folders]]
path = "/home/user/notes"
description = "Personal notes"
```

### Minimal config

Only `[[folders]]` is required — everything else has sensible defaults:

```toml
[[folders]]
path = "/home/user/projects/myapp"
description = "My Go project"
```

This uses Ollama with `nomic-embed-text` at `localhost:11434` and indexes Go, Python, Rust, JavaScript, TypeScript, plus markdown/text files.

### Config reference

| Section | Key | Default | Description |
|---------|-----|---------|-------------|
| `embedding` | `base_url` | `http://localhost:11434/v1` | OpenAI-compatible embedding API |
| `embedding` | `api_key` | `""` | API key (empty for local Ollama) |
| `embedding` | `model` | `nomic-embed-text` | Embedding model name |
| `embedding` | `dimensions` | `768` | Embedding dimensions |
| `embedding` | `batch_size` | `50` | Texts per embedding API call |
| `search` | `rrf_k` | `60` | RRF fusion constant |
| `search` | `similarity_threshold` | `0.3` | Min cosine similarity for vector results |
| `search` | `default_top_k` | `20` | Default number of results |
| `index` | `languages` | `["go","python","rust","javascript","typescript"]` | Languages for tree-sitter chunking |
| `index` | `exclude_patterns` | `[".git","node_modules","vendor","__pycache__",".venv"]` | Directories to skip |
| `index` | `max_file_size_kb` | `512` | Skip files larger than this |

## Usage

### CLI

```bash
# Index all configured folders
poisk index

# Search from the command line
poisk search "authentication middleware"
poisk search "lex:retry backoff lang:go kind:function_declaration"
poisk search "vec:error handling | lex:symbol:OpenDB language:go"

# Show index status
poisk status

# Start MCP server (stdio transport)
poisk serve
```

### MCP Server

Add to your Claude Code MCP config:

```json
{
  "mcpServers": {
    "poisk": {
      "command": "poisk",
      "args": ["serve"]
    }
  }
}
```

#### Tools

- **search** — `{query, top_k?, folders?}` — hybrid semantic + keyword search, optionally filtered to specific folders
- **reindex** — `{folder?, force?}` — re-index configured folders

Typed query syntax:
- `lex:<text>` — keyword-only retrieval
- `vec:<text>` — semantic-only retrieval
- ` | ` — compose multiple sub-queries (e.g. `lex:exact | vec:similar`)
- Metadata filters in any sub-query: `lang:` / `language:`, `kind:` / `chunk_kind:`, `sym:` / `symbol:`

#### Resources

- **poisk://index-status** — JSON with folder stats, file/chunk counts, vec0/FTS5 availability

## Architecture

```
cmd/poisk/           CLI entry point (serve/index/search/status)
internal/
  config/            TOML config parsing
  store/             SQLite + sqlite-vec + FTS5 storage layer (schema v3)
  embed/             OpenAI-compatible embedding client
  chunk/             Tree-sitter AST chunking + markdown section chunking + fixed-window fallback
  index/             Incremental file indexer with mtime tracking
  search/            RRF fusion of vec0 KNN + staged FTS5 BM25
  mcp/               MCP server (tools + resources)
testdata/eval/       Evaluation harness queries (30 gold queries)
```

## Database

SQLite with WAL mode. Data stored at `~/.local/share/poisk/poisk.db`.

- `embedding_files` — file mtime tracking for incremental indexing
- `embeddings` — chunk text + embedding BLOBs (little-endian f32) with metadata (end_line, language, chunk_kind, symbol)
- `embedding_meta` — model/dimensions per source for change detection
- `vec_embeddings` — sqlite-vec virtual table for KNN search
- `chunks_fts` — FTS5 virtual table for keyword search
- `schema_version` — tracks schema version for automatic migration
