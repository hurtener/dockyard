package cli

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/runpkg"
)

// newRunCmd builds the `dockyard run` command — the serve verb (RFC §14,
// §9.1). It is a thin wrapper over internal/runpkg: the build-and-serve logic
// is a reusable, testable package, not logic buried in this RunE.
//
// `dockyard run --transport <stdio|http>` builds the project (a host-only
// 'dockyard build') and runs the produced MCP server binary on the chosen
// transport. Ctrl-C / context cancellation tears the server down cleanly.
func newRunCmd() *cobra.Command {
	var (
		dir       string
		transport string
		addr      string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Build and run the project's MCP server on a transport",
		Long: `Build and run a Dockyard project's MCP server (RFC §14).

'dockyard run' builds the project — a fresh, validated, CGo-free binary, the
same pipeline 'dockyard build' runs — and then runs the produced server on the
transport selected by --transport:

  - stdio  the local single-user subprocess transport (the default);
  - http   the streamable-HTTP transport, listening on --addr; it accepts the
           modern server/discover lifecycle and legacy initialize fallback.

The project's server owns its transport wiring; 'dockyard run' drives it and
never reimplements a transport. Press Ctrl-C to stop: the server child is torn
down cleanly with no orphan process.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			t, err := runpkg.ParseTransport(transport)
			if err != nil {
				return errf("%w", err)
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
				&slog.HandlerOptions{Level: slog.LevelInfo}))

			err = runpkg.Run(cmd.Context(), runpkg.Options{
				ProjectDir: projectDir,
				Transport:  t,
				Addr:       addr,
				Logger:     logger,
			})
			if err != nil {
				return errf("%w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	cmd.Flags().StringVar(&transport, "transport", "stdio",
		"transport to serve on: stdio or http")
	cmd.Flags().StringVar(&addr, "addr", "",
		"HTTP listen address (default 127.0.0.1:8080; ignored for stdio)")
	return cmd
}
