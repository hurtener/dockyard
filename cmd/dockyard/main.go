// Command dockyard is the entrypoint for the Dockyard CLI (RFC §9).
//
// Dockyard ships as one statically-linked, CGo-free binary. This file is a
// thin shell: it owns process-level concerns — signal handling and the exit
// code — and delegates the command tree to internal/cli.
//
// Phase 17 ships the cobra command tree and the `dockyard new` verb. The
// remaining verbs (generate, validate, dev, build, run, install, test) land in
// later Wave 7 phases, each registering itself onto the same root command.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/hurtener/dockyard/internal/cli"

	// Builtin templates register themselves into the process-wide template
	// Registry at init() time (Phase 24, decision D-128). The CLI's
	// `dockyard new --template <name>` verb looks them up by name. Adding a
	// new template is one new blank import here plus one new
	// templates/<name>/ directory.
	_ "github.com/hurtener/dockyard/templates/analytics-widgets"
)

func main() {
	// A cancellable context wired to SIGINT so a long-running verb (a future
	// `dockyard dev`) stops cleanly on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(cli.Execute(ctx))
}
