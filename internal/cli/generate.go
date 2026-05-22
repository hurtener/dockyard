package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
)

// newGenerateCmd builds the `dockyard generate` command — the contract-first
// codegen verb (RFC §6, §9.1). It runs the Design A pipeline over a project's
// Go contract structs: it regenerates the per-contract JSON Schema files and
// the TypeScript contract types. The Go struct is the single source of truth
// (P1); generate produces the rest, never the other way round.
//
// generate is idempotent — a rerun with no contract-source change rewrites the
// same bytes and reports no change.
func newGenerateCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Regenerate JSON Schema and TypeScript from Go contracts",
		Long: `Regenerate a project's contract artifacts from its Go contract structs.

'dockyard generate' runs the Design A codegen pipeline (RFC §6.2): for every
tool in dockyard.app.yaml it generates the input and output JSON Schema and the
TypeScript contract types from the typed Go input/output structs. The Go struct
is the single source of truth — the generated files are never hand-edited.

generate is idempotent: running it twice with no contract change produces a
byte-identical result and reports nothing changed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			m, err := loadProjectManifest(projectDir)
			if err != nil {
				return err
			}
			res, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m})
			if err != nil {
				return errf("generate failed: %w", err)
			}
			printGenerateResult(cmd.OutOrStdout(), res)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	return cmd
}

// resolveProjectDir turns a user-supplied --dir into an absolute project path.
// An empty value resolves to the current working directory.
func resolveProjectDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", errf("resolve project directory %q: %w", dir, err)
	}
	return abs, nil
}

// loadProjectManifest loads and structurally validates a project's
// dockyard.app.yaml. A manifest that will not load is a fatal CLI error: there
// is nothing to generate or validate against.
func loadProjectManifest(projectDir string) (*manifest.Manifest, error) {
	path := filepath.Join(projectDir, manifest.DefaultFilename)
	m, err := manifest.LoadFile(path)
	if err != nil {
		return nil, errf("%w", err)
	}
	return m, nil
}

// printGenerateResult writes the post-generate summary: every file written and
// which of them actually changed. An idempotent rerun reports "no changes".
func printGenerateResult(w io.Writer, res generate.Result) {
	if len(res.Changed) == 0 {
		fmt.Fprintf(w, "generate: %d files up to date, no changes\n", len(res.Written)) //nolint:errcheck // CLI stdout write
		return
	}
	fmt.Fprintf(w, "generate: %d files written, %d changed\n", //nolint:errcheck // CLI stdout write
		len(res.Written), len(res.Changed))
	for _, f := range res.Changed {
		fmt.Fprintf(w, "  changed  %s\n", f) //nolint:errcheck // CLI stdout write
	}
}
