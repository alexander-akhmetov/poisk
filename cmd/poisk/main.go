package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe()
	case "index":
		err = cmdIndex()
	case "search":
		err = cmdSearch()
	case "ask":
		err = cmdAsk()
	case "status":
		err = cmdStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: poisk <command>

Commands:
  serve   Start MCP server (stdio)
  index   Index configured folders
            --watch       Re-index periodically instead of once
            --interval D  Interval between cycles (default 5m)
  search  Search indexed content
  ask     Ask a question using RAG (requires [llm] config)
  status  Show index status
`)
}
