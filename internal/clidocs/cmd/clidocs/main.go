// Command clidocs renders the dockyard CLI reference to a Markdown file.
//
// Usage:
//
//	clidocs -out docs/site/cli/index.md
//
// Invoked by `make docs` so the CLI reference page on the published
// documentation site is always up-to-date with the command tree
// (Phase 29 §19 hygiene).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hurtener/dockyard/internal/clidocs"
)

func main() {
	out := flag.String("out", "", "output Markdown file path (default: stdout)")
	flag.Parse()

	var sink *os.File
	switch *out {
	case "":
		sink = os.Stdout
	default:
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clidocs: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = f.Close() }()
		sink = f
	}
	if err := clidocs.Render(sink); err != nil {
		fmt.Fprintf(os.Stderr, "clidocs: %v\n", err)
		os.Exit(1)
	}
}
