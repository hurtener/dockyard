package cli

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/devloop"
)

// newDevCmd builds the `dockyard dev` command — the embedded fsnotify dev
// orchestrator (RFC §9.2, §9.1). It is a thin wrapper over internal/devloop:
// the orchestration is a reusable package, not logic buried in this RunE.
//
// `dockyard dev` supervises one process tree from a single command — it
// restarts the project's Go MCP server on a .go change, re-runs codegen
// in-process on a contract change, supervises the Vite dev server (which
// owns Svelte HMR), and auto-attaches the local inspector as a third
// supervised child (v1.1 Wave A; closes the V2-backlog auto-attach seam).
// Ctrl-C tears the whole tree down cleanly. Dockyard does not shell out to
// air or wgo — the watcher is embedded (RFC §9.2).
func newDevCmd() *cobra.Command {
	var (
		dir           string
		debounce      time.Duration
		noInspector   bool
		inspectorAddr string
	)

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run the project's dev loop — watch, regenerate, restart, inspect",
		Long: `Run Dockyard's embedded dev loop against a project (RFC §9.2).

'dockyard dev' is one process supervising a process tree. It watches the
project with an embedded fsnotify watcher — no external dev tool — and:

  - restarts the Go MCP server on a .go source change;
  - re-runs contract codegen in-process on a change under internal/contracts,
    so the generated types are live before the server restarts;
  - supervises the Vite dev server for the project's web/ UI (Vite owns Svelte
    HMR). A project with no web/ UI degrades gracefully — only the Go server is
    supervised.
  - auto-attaches the local inspector against the supervised server so the
    Tools / Events / RPC / Verdicts / Prompts panels are one click away. The
    inspector URL is printed to stdout once it is reachable.

By default the dev loop pins the supervised Go server to HTTP on
127.0.0.1:8080 so the inspector has a known MCP base URL to attach to.
A developer who already exported DOCKYARD_TRANSPORT / DOCKYARD_HTTP_ADDR
in their shell wins — the dev-loop pins are defaults, not overrides.

Press Ctrl-C to stop: the whole process tree is torn down cleanly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			// The dev-mode text handler (CLAUDE.md §5) — readable local output.
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
				&slog.HandlerOptions{Level: slog.LevelInfo}))

			err = devloop.Run(cmd.Context(), devloop.Config{
				ProjectDir:       projectDir,
				Logger:           logger,
				Debounce:         debounce,
				DisableInspector: noInspector,
				InspectorAddr:    inspectorAddr,
			})
			if err != nil {
				return errf("dev loop: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	cmd.Flags().DurationVar(&debounce, "debounce", 0,
		"file-change debounce window (default: 250ms)")
	cmd.Flags().BoolVar(&noInspector, "no-inspector", false,
		"do not auto-attach the inspector (for CI / headless dev runs)")
	cmd.Flags().StringVar(&inspectorAddr, "inspector-addr", "",
		"inspector loopback bind (default: 127.0.0.1:0 — OS-assigned port)")
	return cmd
}
