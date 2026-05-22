package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/validate"
)

// newValidateCmd builds the `dockyard validate` command — the quality-gate verb
// (RFC §9.4, §9.1). It runs the project's quality checks — manifest, schemas,
// tool↔UI mappings, MIME, spec compliance, UI states, and stale-codegen drift —
// and exits non-zero when any build-blocker class fails.
//
// validate wraps internal/validate.Run, the reusable validation engine; the
// same Run is the seam `dockyard build` and `dockyard test` consume so the gate
// is defined once.
func newValidateCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Run the project quality gates (manifest, schemas, mappings, spec)",
		Long: `Run Dockyard's quality gates against a project (RFC §9.4).

'dockyard validate' checks the manifest, the generated JSON Schemas, the
tool↔UI resource mappings, the App MIME types, MCP spec compliance against the
vendored specs, the four-state UI page rule, and stale-codegen drift — a
generated file that no longer matches its Go contract source.

A build-blocker failure exits non-zero. Warnings are reported but do not change
the exit code.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			report, err := validate.Run(validate.Options{ProjectDir: projectDir})
			if err != nil {
				return errf("validate could not run: %w", err)
			}
			printValidateReport(cmd.OutOrStdout(), report)
			if report.HasBlockers() {
				// A build-blocker failure is a clean, expected non-zero exit, not
				// an internal CLI error — return a typed sentinel rather than a
				// formatted error so the slog handler does not dump a stack-like
				// noise line over an already-printed, actionable report.
				return errBlockers
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	return cmd
}

// errBlockers is the sentinel `dockyard validate` returns when the Report
// carries build blockers. It maps to a non-zero process exit; the actionable
// detail is already in the printed report.
var errBlockers = errf("validate: build blockers found")

// printValidateReport writes the validation report: blockers first, then
// warnings, then a one-line verdict.
func printValidateReport(w io.Writer, r *validate.Report) {
	blockers := r.Blockers()
	warnings := r.Warnings()

	for _, d := range blockers {
		fmt.Fprintf(w, "  %s\n", d) //nolint:errcheck // CLI stdout write
	}
	for _, d := range warnings {
		fmt.Fprintf(w, "  %s\n", d) //nolint:errcheck // CLI stdout write
	}

	switch {
	case len(blockers) > 0:
		fmt.Fprintf(w, "validate: FAILED — %d build blocker(s), %d warning(s)\n", //nolint:errcheck // CLI stdout write
			len(blockers), len(warnings))
	case len(warnings) > 0:
		fmt.Fprintf(w, "validate: OK — 0 build blockers, %d warning(s)\n", len(warnings)) //nolint:errcheck // CLI stdout write
	default:
		fmt.Fprint(w, "validate: OK — no issues\n") //nolint:errcheck // CLI stdout write
	}
}
