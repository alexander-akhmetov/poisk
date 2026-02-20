# poisk

Hybrid semantic + keyword search over local codebases via [MCP](https://modelcontextprotocol.io/).

Indexes source code and markdown files with embeddings into SQLite ([sqlite-vec](https://github.com/asg017/sqlite-vec) + FTS5) and exposes search through an MCP server that any compatible client (Claude Code, etc.) can use.

## Features

- **Hybrid search** — combines vector similarity (vec0 KNN) with keyword relevance (FTS5 BM25)
- **Incremental indexing** — tracks file mtimes, only re-embeds changed files
- **Model change detection** — automatically rebuilds when embedding model or dimensions change
- **MCP interface** — `search` and `reindex` tools + `poisk://index-status` resource
- **Multiple folders** — index and search across multiple configured directories

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
vector_weight = 0.7
text_weight = 0.3
similarity_threshold = 0.3
default_top_k = 20

[index]
extensions = ["go", "py", "rs", "js", "ts", "md", "txt", "org"]
exclude_patterns = [".git", "node_modules", "vendor", "__pycache__", ".venv"]
max_file_size_kb = 512

[[folders]]
path = "/home/user/projects/myapp"
description = "My application"

[[folders]]
path = "/home/user/notes"
description = "Personal notes"
```

## Usage

### CLI

```bash
# Index all configured folders
poisk index

# Search from the command line
poisk search "authentication middleware"

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

- **search** — `{query, top_k?, folder?}` — hybrid semantic + keyword search
- **reindex** — `{folder?, force?}` — re-index configured folders

#### Resources

- **poisk://index-status** — JSON with folder stats, file/chunk counts, vec0/FTS5 availability

## Architecture

```
cmd/poisk/           CLI entry point (serve/index/search/status)
internal/
  config/            TOML config parsing
  store/             SQLite + sqlite-vec + FTS5 storage layer
  embed/             OpenAI-compatible embedding client
  chunk/             Markdown paragraph + source code chunkers
  index/             Incremental file indexer
  search/            Hybrid vec0 KNN + FTS5 BM25 search
  mcp/               MCP server (tools + resources)
```

## Database

SQLite with WAL mode. Data stored at `~/.local/share/poisk/poisk.db`.

- `embedding_files` — file mtime tracking for incremental indexing
- `embeddings` — chunk text + embedding BLOBs (little-endian f32)
- `embedding_meta` — model/dimensions per source for change detection
- `vec_embeddings` — sqlite-vec virtual table for KNN search
- `chunks_fts` — FTS5 virtual table for keyword search
