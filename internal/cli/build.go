package cli

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/buildpkg"
)

// newBuildCmd builds the `dockyard build` command — the packaging verb
// (RFC §14, §9.1). It is a thin wrapper over internal/buildpkg: the build
// pipeline is a reusable, testable package, not logic buried in this RunE.
//
// `dockyard build` produces the shippable artifact: it regenerates contracts,
// runs the `dockyard validate` quality gate (a validation blocker fails the
// build — P1 at build time), builds the project's web/ Vite UI when one
// exists, then `go build`s one CGo-free static binary per cross-compile target
// with the UI embedded and emits a SHA-256 checksum per artifact.
func newBuildCmd() *cobra.Command {
	var (
		dir          string
		out          string
		crossCompile bool
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the project into a CGo-free static binary with the UI embedded",
		Long: `Build a Dockyard project into its shippable artifact (RFC §14).

'dockyard build' runs the packaging pipeline:

  - regenerates the project's contract artifacts from the Go contracts;
  - runs the 'dockyard validate' quality gate — a build blocker fails the
    build, so a stale or invalid contract never ships;
  - builds the project's web/ Svelte UI with Vite (when the project has one),
    before 'go build' so the embedded dist/ tree exists at compile time;
  - 'go build's one CGo-free, statically-linked binary with the UI embedded.

With --cross-compile it builds the full darwin/linux/windows x amd64/arm64
matrix and emits a SHA-256 checksum file per artifact; otherwise it builds the
host platform only. Artifacts are written to the --output directory (default
dist/).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
				&slog.HandlerOptions{Level: slog.LevelInfo}))

			opts := buildpkg.Options{
				ProjectDir: projectDir,
				OutputDir:  out,
				Logger:     logger,
			}
			if crossCompile {
				opts.Targets = buildpkg.DefaultMatrix()
			}

			res, err := buildpkg.Build(cmd.Context(), opts)
			if err != nil {
				return errf("build failed: %w", err)
			}
			printBuildResult(cmd.OutOrStdout(), res)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	cmd.Flags().StringVar(&out, "output", "",
		"artifact output directory (default: <project>/dist)")
	cmd.Flags().BoolVar(&crossCompile, "cross-compile", false,
		"build the full darwin/linux/windows x amd64/arm64 matrix with checksums")
	return cmd
}

// printBuildResult writes the post-build summary: every artifact, its target,
// and its checksum file.
func printBuildResult(w io.Writer, res buildpkg.Result) {
	fmt.Fprintf(w, "build: %d artifact(s), UI embedded: %t\n", //nolint:errcheck // CLI stdout write
		len(res.Artifacts), res.UIEmbedded)
	for _, a := range res.Artifacts {
		fmt.Fprintf(w, "  %-14s %s\n", a.Target, a.Path)             //nolint:errcheck // CLI stdout write
		fmt.Fprintf(w, "  %-14s %s\n", "  checksum", a.ChecksumPath) //nolint:errcheck // CLI stdout write
	}
}
