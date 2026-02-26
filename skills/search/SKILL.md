---
name: search
description: "Hybrid semantic + keyword search over source code and documents via poisk. Can also help install and configure poisk. Only use when explicitly requested."
allowed-tools: Bash(poisk:*) Bash(go:*) Read
---

# Poisk - Hybrid Search

Search indexed source code and documents using `poisk run` — hybrid semantic + keyword search over indexed content. Can also help install and configure poisk from scratch.

## Installation & Configuration

If the user asks to install or configure poisk, see [references/CONFIGURATION.md](references/CONFIGURATION.md) for installation steps, all config options, and how to create custom skills.

## Usage

```bash
poisk run <query> [--top-k N] [--folders dir1,dir2]
```

Output format per result:
```
[score] file_path:line_num [symbol] (context chain)
Full chunk text...
```

Warnings go to stderr. Use `2>/dev/null` to suppress logs.

## Query Syntax

- Plain text: hybrid semantic + keyword search (default)
- `lex:term`: keyword-only (exact match via FTS5)
- `vec:term`: semantic-only (vector similarity)
- `lex:exact | vec:similar`: compose sub-queries with ` | `
- Metadata filters in any sub-query: `lang:go`, `kind:markdown`, `sym:functionName`

## Search Strategy

1. Start with a broad semantic query to find relevant docs
2. Follow up with `lex:` queries for exact terms (error messages, config keys, function names)
3. Use `--top-k` to control result count (default 20)
4. Use `--folders` to narrow results to a specific directory
5. Read the full file with the Read tool if a chunk looks relevant but you need more context

## Examples

```bash
# Semantic search
poisk run "how does authentication work" --top-k 10 2>/dev/null

# Exact keyword match
poisk run "lex:handleRequest" 2>/dev/null

# Combined: semantic + keyword
poisk run "vec:error handling patterns | lex:error handling" 2>/dev/null

# Filter to specific folder
poisk run "database migrations" --folders ~/projects/myapp 2>/dev/null

# Filter by language
poisk run "lang:go interface implementation" 2>/dev/null
```
