package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "index":
		cmdIndex()
	case "search":
		cmdSearch()
	case "status":
		cmdStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: poisk <command>\n\nCommands:\n  serve   Start MCP server (stdio)\n  index   Index configured folders\n  search  Search indexed content\n  status  Show index status\n")
}
