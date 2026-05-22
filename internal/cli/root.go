package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Version is the dockyard CLI version. It is overridden at build time via
// -ldflags by the release pipeline (Phase 30, RFC §14); the in-tree default is
// a dev placeholder.
var Version = "0.0.0-dev"

// rootLong is the `dockyard` root command's long help. It states the product
// shape — one CGo-free binary, server-side only (P4) — and the verb roadmap so
// `dockyard --help` is honest about what has landed.
const rootLong = `Dockyard is a Go-native framework for building production-grade
MCP Servers and MCP Apps. It ships as one static, CGo-free binary: scaffold a
server, write typed Go tool handlers, and get generated contracts, a local
inspector, quality gates, and one-command packaging.

Phase 17 shipped the command tree and 'dockyard new'; Phase 18 added 'generate'
and 'validate'; Phase 19 adds 'dev'. The remaining verbs — build, run, install,
test — land in later phases.`

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
		Version:       Version,
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
	root.AddCommand(newDevCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newNewCmd())
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
