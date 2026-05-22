package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/testgate"
)

// newTestCmd builds the `dockyard test` command — the contract + compliance
// gate verb (RFC §9.1, §9.4). It runs every test category against a Dockyard
// project as one command — the project's `go test`, the contract-first
// assertions, the fixture/golden snapshots, MCP spec compliance against the
// vendored specs, and capability-degradation tests — and exits non-zero on a
// regression in any gating category.
//
// test wraps internal/testgate.Run, the reusable gate engine; the orchestration
// is a testable package, not logic buried in this RunE — the same pattern as
// `dockyard validate` over internal/validate.Run.
func newTestCmd() *cobra.Command {
	var (
		dir        string
		skipGoTest bool
	)

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run the contract + compliance gate (go test, contracts, spec, capability)",
		Long: `Run Dockyard's full test gate against a project (RFC §9.4).

'dockyard test' runs, as one command, every test category Dockyard's quality
bar is built on:

  - go-test          the project's own Go unit tests ('go test ./...')
  - contract         the contract-first assertions — the generated JSON Schema
                     and TypeScript still match the Go contract structs (P1)
  - golden           the fixture / golden snapshots are present and coherent
  - spec-compliance  the Apps/Tasks constructs conform to the vendored MCP
                     specs (checked against the vendored specs, never a host)
  - capability       the project degrades gracefully across host capability
                     sets — no crash, no hardcoded host matrix (RFC §7.5)

A regression in any gating category exits non-zero. Warnings are reported but
do not change the exit code.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			report, err := testgate.Run(testgate.Options{
				ProjectDir: projectDir,
				SkipGoTest: skipGoTest,
			})
			if err != nil {
				return errf("test gate could not run: %w", err)
			}
			printTestReport(cmd.OutOrStdout(), report)
			if report.Failed() {
				// A category regression is a clean, expected non-zero exit, not
				// an internal CLI error — return a typed sentinel so the slog
				// handler does not dump noise over the already-printed report.
				return errTestFailed
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	cmd.Flags().BoolVar(&skipGoTest, "skip-go-test", false,
		"skip the go-test category (the slowest); the other gates still run")
	return cmd
}

// errTestFailed is the sentinel `dockyard test` returns when the Report carries
// a gating-category regression. It maps to a non-zero process exit; the
// actionable detail is already in the printed report.
var errTestFailed = errf("test: gating category regression")

// printTestReport writes the test-gate report: one verdict line per category,
// then a one-line aggregate verdict.
func printTestReport(w io.Writer, r *testgate.Report) {
	passed := 0
	for _, res := range r.Results {
		fmt.Fprintf(w, "  %s\n", res) //nolint:errcheck // CLI stdout write
		if res.Passed {
			passed++
		}
	}
	if r.Failed() {
		fmt.Fprintf(w, "test: FAILED — %d/%d categories passed\n", //nolint:errcheck // CLI stdout write
			passed, len(r.Results))
		return
	}
	fmt.Fprintf(w, "test: OK — %d/%d categories passed\n", //nolint:errcheck // CLI stdout write
		passed, len(r.Results))
}
