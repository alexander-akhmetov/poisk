package main

import (
	"fmt"
	"log/slog"
	"os"
)

var revision = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex()
	case "run":
		err = cmdRun()
	case "status":
		err = cmdStatus()
	case "version":
		fmt.Println("poisk", revision)
		return
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
  index   Index configured folders
            --watch       Re-index periodically instead of once
            --interval D  Interval between cycles (default 5m)
  run     Search indexed content (full output for scripts/skills)
            --top-k N         Max results (default from config)
            --folders d1,d2   Filter to specific folders
  status  Show index status
            --json        Output as JSON
  version Print version
`)
}
