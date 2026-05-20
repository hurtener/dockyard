// Command dockyard is the entrypoint for the Dockyard CLI / generator.
//
// Phase 01 ships a placeholder: the cobra command tree, project scaffolding,
// codegen, the dev loop, and the inspector land in Wave 7+ (RFC §9, master
// plan phases 17–23). The placeholder exists now so the module layout matches
// AGENTS.md §3 and `make build` produces a CGo-free binary from Phase 01 on.
package main

import (
	"fmt"
	"os"
)

// version is the Dockyard CLI version. It is a build-time placeholder until
// Phase 30 wires release versioning (RFC §14).
const version = "0.0.0-dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("dockyard %s\n", version)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "dockyard: CLI not yet implemented — see RFC §9 (lands in phase 17+)")
	fmt.Fprintf(os.Stderr, "dockyard %s\n", version)
	os.Exit(1)
}
