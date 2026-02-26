# poisk

[![build](https://github.com/alexander-akhmetov/poisk/actions/workflows/ci.yml/badge.svg)](https://github.com/alexander-akhmetov/poisk/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/alexander-akhmetov/poisk/graph/badge.svg)](https://codecov.io/gh/alexander-akhmetov/poisk)
[![Go Report Card](https://goreportcard.com/badge/github.com/alexander-akhmetov/poisk)](https://goreportcard.com/report/github.com/alexander-akhmetov/poisk)

Hybrid semantic + keyword search over local files and codebases via CLI and [MCP](https://modelcontextprotocol.io/).

Indexes source code and markdown files with embeddings into SQLite ([sqlite-vec](https://github.com/asg017/sqlite-vec) + FTS5) and exposes search through a CLI and MCP server that any compatible client (Claude Code, etc.) can use.

## Install with Claude Code

The easiest way to get started is with the [Claude Code](https://docs.anthropic.com/en/docs/claude-code) plugin. It installs a skill that teaches Claude how to use poisk and helps you set everything up.

**1. Install the plugin:**

```bash
claude plugin marketplace add alexander-akhmetov/poisk
claude plugin install -s user poisk
```

**2. Ask Claude to install and configure poisk:**

Claude will help you install the `poisk` binary, create the config file, set up folders, and run the initial index. Just ask it — for example: *"install and configure poisk for my projects"*.

**3. Use it:**

Ask Claude to search with poisk (e.g. *"use poisk to search for authentication middleware"*), or create custom skills that invoke poisk for specific workflows — for example, a skill that searches your notes folder whenever you ask a question about a topic. See the [configuration reference](skills/setup/SKILL.md) for examples of custom skills.

## How it works

poisk stores everything in a local SQLite database (`~/.local/share/poisk/poisk.db`). During indexing, source code files are parsed with [tree-sitter](https://tree-sitter.github.io/) into AST-aware chunks (functions, structs, etc.), markdown files are split by headings into sections, and each chunk is embedded via any OpenAI-compatible API (Ollama by default). The embeddings go into [sqlite-vec](https://github.com/asg017/sqlite-vec) for vector search, and the raw text goes into FTS5 for keyword search.

At query time, both results are merged with Reciprocal Rank Fusion — so you get semantic understanding and exact keyword matches in one search. Indexing is incremental: only changed files (by mtime) are re-processed.



## Features

- **Hybrid search** — combines vector similarity (vec0 KNN) with keyword relevance (FTS5 BM25), merged via Reciprocal Rank Fusion
- **Code-aware FTS** — tokenizer splits camelCase/snake_case, staged retrieval (strict AND → relaxed OR → prefix OR)
- **Tree-sitter chunking** — AST-based code chunking for Go, Python, Rust, JavaScript, TypeScript (with JSX/TSX)
- **Markdown chunking** — heading-aware sections with breadcrumb paths, fence-aware splitting
- **Incremental indexing** — tracks file mtimes, only re-embeds changed files
- **Multiple folders** — index and search across multiple configured directories

## Manual install

Requires Go 1.26+ and CGO (for sqlite3 + sqlite-vec).

```bash
go install -tags fts5 github.com/alexander-akhmetov/poisk/cmd/poisk@latest
```

Or build from source:

```bash
git clone https://github.com/alexander-akhmetov/poisk.git
cd poisk
make build
```

### Configuration

poisk needs an OpenAI-compatible embedding API. By default it uses [Ollama](https://ollama.com/) with [`qwen3-embedding:4b`](https://ollama.com/library/qwen3-embedding:4b) at `localhost:11434`.

Create `~/.config/poisk/config.toml`. Only `[[folders]]` is required — everything else has sensible defaults:

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

See [config.example.toml](config.example.toml) for all options, or the full [configuration reference](skills/setup/SKILL.md).

### Usage

```bash
# Index all configured folders
poisk index

# Search from the command line
poisk run "authentication middleware"
poisk run "lex:retry backoff lang:go kind:function_declaration"

# Show index status
poisk status

# Start MCP server (stdio transport)
poisk serve
```

### Query syntax

By default queries use hybrid search (semantic + keyword). You can control this:

- `lex:<text>` — keyword-only
- `vec:<text>` — semantic-only
- ` | ` — compose sub-queries (e.g. `lex:exact_name | vec:what does it do`)
- Filters: `lang:go`, `kind:function_declaration`, `symbol:OpenDB`
