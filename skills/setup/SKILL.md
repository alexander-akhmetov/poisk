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
| `dimensions` | `1024` | Embedding vector dimensions (qwen3-embedding supports 32–4096). Also a latency lever, see below |
| `max_input_bytes` | `8000` | Byte ceiling for one embedding input. Chunk text longer than this is split. Accepted range 4 to 8000 |
| `batch_size` | `50` | Inputs per embedding request |
| `batch_max_bytes` | `65536` | Summed raw chunk text per embedding request. Must be >= `max_input_bytes` |
| `send_dimensions` | `true` | Include dimensions parameter in API requests |
| `quantization` | `"int8"` | Vector storage type: `"int8"` (~4x smaller index) or `"float32"` |
| `matryoshka` | `false` | Truncate longer API vectors to `dimensions` and renormalize — for providers that ignore the dimensions parameter (e.g. Ollama) |

**Three size limits, three units.** `max_input_bytes` splits one chunk's text so no single input can occupy the provider for minutes. `batch_max_bytes` partitions requests by summed raw chunk text; it does not cap the JSON body poisk sends or the tokens the provider counts, both of which are larger. `[index].max_file_size_kb` skips whole files before any chunking happens.

Changing `max_input_bytes` re-chunks and re-embeds every folder, the way `model`, `dimensions` and `quantization` already do, because the stored chunks were cut to the old limit.

**`dimensions` and search latency.** sqlite-vec has no approximate index, so every query scans every vector and the cost is linear in dimensions. Measured on 200k vectors at k=100:

| `dimensions` | vector search |
|---|---|
| 1024 | 219 ms |
| 512 | 111 ms |
| 256 | 58 ms |

Halving `dimensions` roughly halves both search time and index size. How much recall that costs depends on the model, so treat 512 as a lever to evaluate against your own corpus, not a recommendation. The default stays 1024. With a Matryoshka-trained model (like qwen3-embedding) on a provider that ignores the `dimensions` parameter, set `matryoshka = true` so poisk truncates and renormalizes the longer vectors it receives.

**Full reindex triggers.** Changing `model`, `dimensions`, or `quantization` re-embeds every folder. A poisk upgrade that bumps the internal schema version can also force one; targeted migrations preserve the existing vectors where a path exists.

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
| `embedding_timeout` | `5s` | Query embedding deadline; hybrid search falls back to keyword results on timeout |
| `vec_weight` | `1.0` | Vector search contribution multiplier |
| `fts_weight` | `1.1` | Keyword search contribution multiplier |
| `original_query_weight` | `1.0` | Weight for the original query |
| `expanded_query_weight` | `0.25` | Weight for LLM-expanded query variants |
| `query_expansion` | `false` | Enable LLM query expansion (requires `[llm]`) |
| `rerank` | `true` | Enable LLM reranking (requires `[llm]`) |
| `rerank_top_n` | `20` | Number of candidates to rerank |
| `rerank_retrieval_weight_top` | `0.8` | Retrieval signal blend for top results (0.0–1.0) |
| `rerank_retrieval_weight_bottom` | `0.2` | Retrieval signal blend for bottom results (0.0–1.0) |

`embedding_timeout` accepts Go duration strings such as `500ms`, `5s`, or `1m`. It applies only to query embeddings. Indexing keeps its longer request timeout. A timed-out `vec:` query returns an error because it has no keyword search to fall back to.

### `[llm]`

Optional. Enables reranking and query expansion.

| Key | Default | Description |
|-----|---------|-------------|
| `base_url` | — | OpenAI-compatible chat API endpoint |
| `api_key` | `""` | API key |
| `model` | — | Chat model name |
| `extra_body` | — | Extra fields merged into every chat completion request |

```toml
[llm]
base_url = "http://localhost:11434/v1"
model = "llama3"
```

#### Reasoning models

Query expansion and reranking ask for a short answer under a small token budget (200 and 100 tokens). A reasoning model spends that budget on thinking and returns empty content, so poisk waits for the model and then throws the answer away: expansion falls back to the original query and reranking keeps the retrieval order. Each search pays several seconds for nothing.

poisk handles the Qwen3 family on its own. When the model name matches (`qwen3`, `qwen-3`, at any routing prefix such as `mac-studio/qwen3.6-35b-a3b`), it adds `chat_template_kwargs = { enable_thinking = false }` to every request. If the server answers 4xx, poisk drops the field, retries without it, and stops sending it, so the name match cannot break a server that does not know the field.

For any other reasoning model, set it yourself. On llama.cpp and vLLM:

```toml
[llm.extra_body.chat_template_kwargs]
enable_thinking = false
```

Some servers take `reasoning_effort = "none"` instead. An explicit `chat_template_kwargs` always wins, so this is also how to turn thinking back **on** for a Qwen3 model:

```toml
[llm.extra_body.chat_template_kwargs]
enable_thinking = true
```

If no switch works, set `rerank = false` and `query_expansion = false` rather than paying for stages that cannot produce an answer. An empty completion is reported as an error naming this cause, so the log says which stage was skipped and why.

### `[server]`

Optional. Used by `poisk serve --http` (MCP over Streamable HTTP for remote clients). Stdio mode ignores this section.

| Key | Default | Description |
|-----|---------|-------------|
| `listen` | `127.0.0.1:8765` | Listen address; override per run with `--listen` |
| `token` | `""` | Bearer token required on every request; falls back to `POISK_SERVER_TOKEN` env var. HTTP mode refuses to start without a token |

```toml
[server]
listen = "127.0.0.1:8765"
token = "secret"
```

The token grants read access to all indexed folders. poisk serves plain HTTP — for non-localhost access, put it behind a reverse proxy or tunnel that terminates TLS.

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
max_input_bytes = 8000
batch_size = 50
batch_max_bytes = 65536
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
poisk serve --http               # Serve MCP over HTTP with bearer-token auth
poisk serve --http --listen 127.0.0.1:9000  # Custom listen address
```
