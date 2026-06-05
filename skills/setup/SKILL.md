---
name: setup
description: "Install, configure, and manage poisk — hybrid search over local files and codebases. Use when the user asks to install poisk, set up indexing, change config, or create custom search skills."
allowed-tools: Bash(poisk:*) Bash(go:*) Read Write
---

# Poisk Setup

Help the user install, configure, and manage poisk.

## Installation

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

### Setup steps

1. **Install the binary** (see above)
2. **Set up an embedding provider** — poisk needs an OpenAI-compatible embedding API to generate vectors. Ask the user which option they prefer:
   - **Ollama (default, free, local)** — install [Ollama](https://ollama.com/), then pull the model:
     ```bash
     ollama pull qwen3-embedding:4b    # 2.5 GB, recommended default
     # or for lower resource usage:
     ollama pull qwen3-embedding:0.6b  # 639 MB, lighter alternative
     ```
     No API key needed. The default config points at Ollama's local endpoint (`http://localhost:11434/v1`).
   - **OpenAI** — use OpenAI's embedding API. Requires an API key. Set `base_url`, `api_key`, `model`, and `dimensions` in the `[embedding]` section (see below).
   - **Any OpenAI-compatible API** — LM Studio, vLLM, etc. — just point `base_url` at the endpoint.
3. **Create the config** at `~/.config/poisk/config.toml` — ask the user which folders to index. Minimal config (works with Ollama defaults):
   ```toml
   [[folders]]
   path = "~/projects/myapp"
   description = "My application"
   ```
4. **Run initial index**: `poisk index`
5. **Verify**: `poisk status`

## Configuration

Config file: `~/.config/poisk/config.toml` (or `$XDG_CONFIG_HOME/poisk/config.toml`).

Database: `~/.local/share/poisk/poisk.db` (or `$XDG_DATA_HOME/poisk/poisk.db`).

Only `[[folders]]` is required — everything else has sensible defaults.

### `[embedding]`

Any OpenAI-compatible embedding API. Defaults to local Ollama with `qwen3-embedding:4b`.

| Key | Default | Description |
|-----|---------|-------------|
| `base_url` | `http://localhost:11434/v1` | API endpoint |
| `api_key` | `""` | API key (empty for local Ollama) |
| `model` | `qwen3-embedding:4b` | Embedding model name |
| `dimensions` | `1024` | Embedding vector dimensions (qwen3-embedding supports 32–4096) |
| `batch_size` | `50` | Chunks per embedding request |
| `send_dimensions` | `true` | Include dimensions parameter in API requests |

Example with OpenAI:

```toml
[embedding]
base_url = "https://api.openai.com/v1"
api_key = "sk-..."
model = "text-embedding-3-small"
dimensions = 1536
send_dimensions = true
```

### `[index]`

Global indexing settings. Per-folder overrides take precedence when set.

| Key | Default | Description |
|-----|---------|-------------|
| `exclude_patterns` | `[".git", "node_modules", "vendor", "__pycache__", ".venv"]` | Directories/patterns to skip |
| `include_patterns` | `[]` (all supported extensions) | File glob patterns to include |
| `max_file_size_kb` | `512` | Skip files larger than this |

Supported languages for tree-sitter AST chunking: Go, Python, Rust, JavaScript, TypeScript (with JSX/TSX), Common Lisp. Markdown, text, and org files are always indexed.

### `[search]`

Search tuning. Defaults work well for most cases.

| Key | Default | Description |
|-----|---------|-------------|
| `rrf_k` | `60` | RRF constant (higher = more weight to top-ranked) |
| `similarity_threshold` | `0.3` | Minimum cosine similarity (0.0–1.0) |
| `default_top_k` | `20` | Default number of results |
| `vec_weight` | `1.0` | Vector search contribution multiplier |
| `fts_weight` | `1.1` | Keyword search contribution multiplier |
| `original_query_weight` | `1.0` | Weight for the original query |
| `expanded_query_weight` | `0.25` | Weight for LLM-expanded query variants |
| `query_expansion` | `false` | Enable LLM query expansion (requires `[llm]`) |
| `rerank` | `true` | Enable LLM reranking (requires `[llm]`) |
| `rerank_top_n` | `20` | Number of candidates to rerank |
| `rerank_retrieval_weight_top` | `0.8` | Retrieval signal blend for top results (0.0–1.0) |
| `rerank_retrieval_weight_bottom` | `0.2` | Retrieval signal blend for bottom results (0.0–1.0) |

### `[llm]`

Optional. Enables reranking and query expansion.

| Key | Default | Description |
|-----|---------|-------------|
| `base_url` | — | OpenAI-compatible chat API endpoint |
| `api_key` | `""` | API key |
| `model` | — | Chat model name |

```toml
[llm]
base_url = "http://localhost:11434/v1"
model = "llama3"
```

### `[[folders]]`

One or more folders to index. Each is indexed independently.

| Key | Required | Description |
|-----|----------|-------------|
| `path` | yes | Directory path (supports `~` expansion) |
| `description` | no | Human-readable label |
| `exclude_patterns` | no | Override global `[index].exclude_patterns` |
| `include_patterns` | no | Override global `[index].include_patterns` |
| `max_file_size_kb` | no | Override global `[index].max_file_size_kb` (when > 0) |

```toml
[[folders]]
path = "~/projects/myapp"
description = "My application"
exclude_patterns = ["testdata", "vendor"]

[[folders]]
path = "~/notes"
description = "Personal notes"
include_patterns = ["*.md", "*.txt", "*.org"]
```

### Indexing coding-agent sessions

poisk can index conversation transcripts from Claude Code and pi as searchable
turn chunks (one chunk per user+assistant exchange). Each turn is stored with
`lang:session` and `kind:turn`, so you can scope searches with
`poisk run "lang:session kind:turn <what you remember>"`. Only user and
assistant text is kept; thinking, tool calls, and tool results are dropped.

pi stores one `.jsonl` per session under `~/.pi/agent/sessions/`. To index them:

```toml
[[folders]]
path = "~/.pi/agent/sessions"
description = "pi coding agent sessions"
include_patterns = ["*.jsonl"]
max_file_size_kb = 4096
```

Two things matter here:

- `include_patterns = ["*.jsonl"]` is required. `.jsonl` is not in the default
  extension set, so without it the sessions are skipped. Scoping it to this
  folder also keeps unrelated `.jsonl` data files elsewhere out of the index.
- `max_file_size_kb` must be raised. Session files routinely run 300KB to 1MB+,
  well past the 512KB default, and oversized files are silently skipped at scan
  time. The per-folder override lets sessions use a larger cap while code
  folders keep the smaller default.

## Full example

```toml
[embedding]
base_url = "http://localhost:11434/v1"
model = "qwen3-embedding:4b"
dimensions = 1024
batch_size = 50
send_dimensions = true

[search]
default_top_k = 20
similarity_threshold = 0.3
rerank = true

[index]
exclude_patterns = [".git", "node_modules", "vendor", "__pycache__", ".venv", "dist", "build"]
max_file_size_kb = 512

[llm]
base_url = "http://localhost:11434/v1"
model = "llama3"

[[folders]]
path = "~/projects/myapp"
description = "My application"

[[folders]]
path = "~/notes"
description = "Personal notes"
include_patterns = ["*.md", "*.txt", "*.org"]
```

## Creating a skill to invoke poisk

The plugin provides a generic search skill. You can create custom skills that invoke poisk for specific workflows. For example, a skill that searches your notes before answering questions:

Create `~/.claude/skills/search-notes/SKILL.md`:

````markdown
---
name: search-notes
description: "Search personal notes before answering knowledge questions"
allowed-tools: Bash(poisk:*), Read
---

When the user asks a knowledge question, search notes first:

```bash
poisk run "<query>" --folders ~/notes --top-k 5 2>/dev/null
```

Then read the relevant files for full context and answer the question.
````

Or a skill for searching a specific project's codebase:

````markdown
---
name: search-myapp
description: "Search myapp codebase"
allowed-tools: Bash(poisk:*)
---

Search the myapp codebase:

```bash
poisk run "<query>" --folders ~/projects/myapp --top-k 10 2>/dev/null
```
````

## CLI commands

```bash
poisk index                      # Index all configured folders (incremental, skips unchanged files)
poisk index --watch              # Keep running and re-index periodically (default every 5m)
poisk index --watch --interval 2m  # Custom watch interval
poisk run "<query>"              # Search
poisk status                     # Show index status
poisk serve                      # Start MCP server (stdio)
```
