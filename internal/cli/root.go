package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the dockyard CLI version. It is overridden at build time via
// -ldflags -X by the release pipeline (internal/releasebuild); the in-tree
// default is a dev placeholder. Prefer ResolvedVersion() over reading this
// directly — it also recovers the module version from the build info for a
// `go install …@vX.Y.Z` binary, which carries no ldflags stamp.
var Version = "0.0.0-dev"

// devVersion is the in-tree placeholder — anything else is a real build.
const devVersion = "0.0.0-dev"

// ResolvedVersion returns the best-known CLI version, in order of confidence:
//  1. the -ldflags -X stamp (the release-pipeline cross-compiled binaries);
//  2. the module version from the build info (a `go install …@vX.Y.Z` binary —
//     it has no ldflags stamp but Go records the module version);
//  3. the dev placeholder (a `make build` / `go build` from a checkout).
//
// It is what `dockyard --version` reports and what `dockyard new` pins into a
// scaffolded go.mod's require directive (so the published-module path resolves
// without a hand edit).
func ResolvedVersion() string {
	if Version != "" && Version != devVersion {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// rootLong is the `dockyard` root command's long help. It states the product
// shape — one CGo-free binary, server-side only (P4) — and the verb roadmap so
// `dockyard --help` is honest about what has landed.
const rootLong = `Dockyard is a Go-native framework for building production-grade
MCP Servers and MCP Apps. It ships as one static, CGo-free binary: scaffold a
server, write typed Go tool handlers, and get generated contracts, a local
inspector, quality gates, and one-command packaging.

The command tree covers the full developer workflow: 'new' scaffolds a server,
'generate' regenerates contracts from typed Go structs, 'validate' enforces
the contract-first quality gate, 'dev' runs the live-reload loop, 'build'
produces the shippable binary, 'run' serves it, 'install' registers it with
a host, 'test' runs the contract + compliance gate, and 'inspect' opens the
local debug surface.`

// NewRootCmd builds the root `dockyard` cobra command with every subcommand
// that has landed registered onto it. It is the single composition point: a
// later Wave 7 phase adds its verb with one root.AddCommand line here and one
// command-constructor file, never a tree restructure.
//
// stdout/stderr are injected so the command tree is testable without touching
// the process streams; pass os.Stdout / os.Stderr in main.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "dockyard",
		Short: "Build production-grade MCP Servers and MCP Apps",
		Long:  rootLong,
		// The CLI reports its own errors through the slog text handler and a
		// non-zero exit; cobra's default usage-on-error noise is suppressed so
		// a genuine failure is not buried under a usage dump.
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       ResolvedVersion(),
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	// A bare `dockyard` prints help rather than erroring — the friendly default
	// for a DX-first tool (brief 04 §2).
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}

	// Subcommand registration. Each landed verb is one line; later phases add
	// theirs here. Keep alphabetical.
	root.AddCommand(newBuildCmd())
	root.AddCommand(newDevCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newInstallCmd())
	root.AddCommand(newNewCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newValidateCmd())

	return root
}

// Execute builds the root command and runs it against os.Args, returning the
// process exit code. main is a thin wrapper around it.
//
// A command failure is logged through a log/slog text handler — the dev-mode
// handler mandated by CLAUDE.md §5 — and mapped to exit code 1. Execute never
// panics: a misuse surfaces as a typed error and a clean non-zero exit.
func Execute(ctx context.Context) int {
	root := NewRootCmd(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		logger.ErrorContext(ctx, "dockyard command failed", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

// errf formats a CLI-facing error. It is the small shared helper command
// constructors use so every verb's errors read consistently.
func errf(format string, args ...any) error {
	return fmt.Errorf("dockyard: "+format, args...)
}
