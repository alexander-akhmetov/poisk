# poisk

Hybrid semantic + keyword search over local codebases via [MCP](https://modelcontextprotocol.io/).

Indexes source code and markdown files with embeddings into SQLite ([sqlite-vec](https://github.com/asg017/sqlite-vec) + FTS5) and exposes search through an MCP server that any compatible client (Claude Code, etc.) can use.

## Features

- **Hybrid search** — combines vector similarity (vec0 KNN) with keyword relevance (FTS5 BM25), merged via Reciprocal Rank Fusion
- **Code-aware FTS** — tokenizer splits camelCase/snake_case, staged retrieval (strict AND → relaxed OR → prefix OR)
- **Tree-sitter chunking** — AST-based code chunking for Go, Python, Rust, JavaScript, TypeScript (with JSX/TSX)
- **Markdown chunking** — heading-aware sections with breadcrumb paths, fence-aware splitting
- **Incremental indexing** — tracks file mtimes, only re-embeds changed files
- **Multiple folders** — index and search across multiple configured directories

## How it works

poisk stores everything in a local SQLite database (`~/.local/share/poisk/poisk.db`). During indexing, source code files are parsed with [tree-sitter](https://tree-sitter.github.io/) into AST-aware chunks (functions, structs, etc.), markdown files are split by headings into sections, and each chunk is embedded via any OpenAI-compatible API (Ollama by default). The embeddings go into [sqlite-vec](https://github.com/asg017/sqlite-vec) for vector search, and the raw text goes into FTS5 for keyword search.

At query time, both results are merged with Reciprocal Rank Fusion — so you get semantic understanding and exact keyword matches in one search. Indexing is incremental: only changed files (by mtime) are re-processed.

## Install

Requires Go 1.26+ and CGO (for sqlite3 + sqlite-vec).

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

Create `~/.config/poisk/config.toml`. Only `[[folders]]` is required — everything else has sensible defaults (Ollama with `nomic-embed-text` at `localhost:11434`):

```toml
[[folders]]
path = "/home/user/projects/myapp"
description = "My application"

[[folders]]
path = "/home/user/notes"
description = "Personal notes"
```

To use a different embedding provider, add an `[embedding]` section:

```toml
[embedding]
base_url = "https://api.openai.com/v1"
api_key = "sk-..."
model = "text-embedding-3-small"
dimensions = 1536
```

## Usage

### CLI

```bash
# Index all configured folders
poisk index

# Search from the command line
poisk search "authentication middleware"
poisk search "lex:retry backoff lang:go kind:function_declaration"

# Show index status
poisk status

# Start MCP server (stdio transport)
poisk serve
```

### Claude Code Plugin

The easiest way to use poisk with Claude Code is as a plugin:

```bash
claude plugin add alexander-akhmetov/poisk
```

This registers the MCP server automatically. After installing, index your folders and the `search` / `reindex` tools become available in Claude Code.

### MCP Server (manual setup)

If you prefer manual configuration, add to your MCP settings (e.g. `.claude/settings.json`):

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

### Query syntax

By default queries use hybrid search (semantic + keyword). You can control this:

- `lex:<text>` — keyword-only
- `vec:<text>` — semantic-only
- ` | ` — compose sub-queries (e.g. `lex:exact_name | vec:what does it do`)
- Filters: `lang:go`, `kind:function_declaration`, `symbol:OpenDB`
