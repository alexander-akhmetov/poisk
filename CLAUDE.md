# poisk

Hybrid search with MCP interface.

## Build & Test

```bash
make build    # builds with -tags fts5
make test     # runs all tests
```

The `-tags fts5` build tag is required for FTS5 support in go-sqlite3.
