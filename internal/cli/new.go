package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// newNewCmd builds the `dockyard new` command — the no-template project
// scaffold (RFC §9.1, §10). It is the one verb Phase 17 ships; the rest of the
// command tree's verbs land in later Wave 7 phases.
//
// `dockyard new <name>` with no --template produces a blank but working MCP
// server: a manifest, one example contract-first tool, the generated contract
// artifacts, a runnable main and a contract test. The no-template path is the
// first-class one (RFC §10).
func newNewCmd() *cobra.Command {
	var (
		dir          string
		modulePath   string
		dockyardPath string
		templateName string
	)

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a new MCP server project",
		Long: `Scaffold a new, blank, working MCP server.

'dockyard new <name>' creates a project directory with a manifest, one example
contract-first tool, its generated JSON Schema and TypeScript, a runnable main,
and a contract test. The generated project builds and serves immediately.

The no-template path is first-class — no --template flag is required to get a
working server. Templates (analytics-widgets, approval-flow, inspector) are
optional product-pattern showcases; pass --template <name> to scaffold one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			resolvedDockyard, err := resolveDockyardPath(dockyardPath)
			if err != nil {
				return err
			}
			// When --dockyard-path is set, default the web sibling to
			// <dockyard-path>/web — every template that needs the in-repo
			// @dockyard/bridge / @dockyard/ui packages then resolves them.
			// Pre-publish only: a released Dockyard leaves both empty.
			resolvedWeb := ""
			if resolvedDockyard != "" {
				resolvedWeb = filepath.Join(resolvedDockyard, "web")
			}
			opts := scaffold.Options{
				Name:            name,
				Dir:             dir,
				ModulePath:      modulePath,
				DockyardReplace: resolvedDockyard,
				DockyardWebPath: resolvedWeb,
			}
			var res scaffold.Result
			if templateName == "" {
				res, err = scaffold.Generate(opts)
			} else {
				res, err = scaffold.GenerateFromTemplate(opts, templateName)
			}
			if err != nil {
				return mapScaffoldError(err)
			}
			printResult(cmd.OutOrStdout(), res)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"parent directory to create the project under (default: current directory)")
	cmd.Flags().StringVar(&modulePath, "module", "",
		"Go module path for the new project's go.mod (default: example.com/<name>)")
	cmd.Flags().StringVar(&templateName, "template", "",
		"product-pattern template to scaffold (e.g. analytics-widgets). "+
			"Omit for the blank no-template scaffold (the first-class path).")
	// --dockyard-path is the pre-release seam: until Dockyard is published to a
	// module registry, a scaffolded project needs a `replace` directive
	// pointing at a local Dockyard checkout to compile. It is hidden because a
	// released Dockyard CLI will not need it.
	cmd.Flags().StringVar(&dockyardPath, "dockyard-path", "",
		"local path to the Dockyard runtime checkout (pre-release; adds a go.mod replace)")
	_ = cmd.Flags().MarkHidden("dockyard-path")

	return cmd
}

// resolveDockyardPath turns a user-supplied --dockyard-path into an absolute
// path for the scaffolded go.mod `replace` directive. An empty value is passed
// through unchanged (no replace directive — the released-module workflow).
func resolveDockyardPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", errf("resolve --dockyard-path %q: %w", p, err)
	}
	return abs, nil
}

// mapScaffoldError turns a scaffold sentinel error into a CLI-facing message
// with an actionable hint.
func mapScaffoldError(err error) error {
	switch {
	case errors.Is(err, scaffold.ErrInvalidName):
		return errf("%w", err)
	case errors.Is(err, scaffold.ErrTargetExists):
		return errf("%w — choose another name or remove the directory", err)
	case errors.Is(err, scaffold.ErrUnknownTemplate):
		return errf("%w — run `dockyard new --help` for the registered set", err)
	default:
		return errf("scaffold failed: %w", err)
	}
}

// printResult writes the post-scaffold summary: the directory, the file list,
// and the next-step commands. Writes to a CLI output stream go to a buffer or
// os.Stdout — a write error there is not actionable, so it is intentionally
// not surfaced.
func printResult(w io.Writer, res scaffold.Result) {
	fmt.Fprintf(w, "Created %s (%d files)\n", res.Dir, len(res.Files)) //nolint:errcheck // CLI stdout write
	for _, f := range res.Files {
		fmt.Fprintf(w, "  %s\n", f) //nolint:errcheck // CLI stdout write
	}
	fmt.Fprint(w, "\nNext steps:\n")                              //nolint:errcheck // CLI stdout write
	fmt.Fprintf(w, "  cd %s\n", res.Dir)                          //nolint:errcheck // CLI stdout write
	fmt.Fprint(w, "  go test ./...   # run the contract tests\n") //nolint:errcheck // CLI stdout write
	fmt.Fprint(w, "  go run .        # serve over stdio\n")       //nolint:errcheck // CLI stdout write
}
