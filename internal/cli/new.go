package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
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
		dir                string
		modulePath         string
		dockyardPath       string
		templateName       string
		exampleTaskSupport string
		noPostgen          bool
		here               bool
	)

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a new MCP server project",
		Long: `Scaffold a new, blank, working MCP server.

'dockyard new <name>' creates a project directory with a manifest, one example
contract-first tool, its generated JSON Schema and TypeScript, a runnable main,
and a contract test. The generated project builds and serves immediately.

The no-template path is first-class — no --template flag is required to get a
working server. Templates (analytics-widgets, approval-flows) are optional
product-pattern showcases; pass --template <name> to scaffold one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			resolvedDockyard, err := resolveDockyardPath(dockyardPath)
			if err != nil {
				return err
			}
			// When --dockyard-path is set, default the web sibling to
			// <dockyard-path>/web — every template that needs the in-repo
			// dockyard-bridge / dockyard-ui packages then resolves them.
			// Pre-publish only: a released Dockyard leaves both empty.
			resolvedWeb := ""
			if resolvedDockyard != "" {
				resolvedWeb = filepath.Join(resolvedDockyard, "web")
			}
			taskSupport, tsErr := parseExampleTaskSupport(exampleTaskSupport)
			if tsErr != nil {
				return tsErr
			}
			opts := scaffold.Options{
				Name:                   name,
				Dir:                    dir,
				ModulePath:             modulePath,
				DockyardReplace:        resolvedDockyard,
				DockyardWebPath:        resolvedWeb,
				ExampleToolTaskSupport: taskSupport,
				DockyardVersion:        ResolvedVersion(),
				Here:                   here,
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
			// Post-scaffold steps (D-166, supersedes D-139): run `go mod tidy`
			// + `dockyard generate` so a fresh project is green under
			// `dockyard validate` on the first try, with no manual commands.
			// Best-effort — a failure (typically no module-proxy reach) warns
			// and points at the manual fallback rather than failing the
			// scaffold. --no-postgen skips both for hermetic / air-gapped runs.
			ranPostgen := !noPostgen
			if ranPostgen {
				ranPostgen = runPostScaffold(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), res.Dir)
			}
			printNextSteps(cmd.OutOrStdout(), res.Dir, ranPostgen)
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
	// --example-task-support opts the no-template scaffold's example tool
	// into a non-default task_support declaration. The blank default is
	// "forbidden" (the historical shape); "optional" / "required" makes the
	// scaffold both write the corresponding manifest line AND emit the
	// engine-wired main.go that constructs a real tasks.Engine and attaches
	// it via server.Options.Tasks (D-164).
	cmd.Flags().StringVar(&exampleTaskSupport, "example-task-support", "",
		"example tool's task_support declaration: forbidden (default), optional, or required. "+
			"optional/required also auto-wires a tasks.Engine in main.go.")
	// --dockyard-path is the pre-release seam: until Dockyard is published to a
	// module registry, a scaffolded project needs a `replace` directive
	// pointing at a local Dockyard checkout to compile. It is hidden because a
	// released Dockyard CLI will not need it.
	cmd.Flags().StringVar(&dockyardPath, "dockyard-path", "",
		"local path to the Dockyard runtime checkout (pre-release; adds a go.mod replace)")
	_ = cmd.Flags().MarkHidden("dockyard-path")
	// --no-postgen skips the post-scaffold `go mod tidy` + `dockyard generate`
	// steps. Default false: a fresh scaffold is green under `dockyard validate`
	// on the first try (D-166). Opt out for hermetic / air-gapped / CI runs
	// where the module proxy is unreachable or the steps are run separately.
	cmd.Flags().BoolVar(&noPostgen, "no-postgen", false,
		"skip the post-scaffold `go mod tidy` + `dockyard generate` steps")
	// --here scaffolds into an existing non-empty directory (e.g. one you
	// already `git init`-ed). Existing files are left untouched; a scaffold
	// output that would overwrite an existing file is still refused.
	cmd.Flags().BoolVar(&here, "here", false,
		"scaffold into the target directory even if it already has content (never overwrites a file)")

	return cmd
}

// parseExampleTaskSupport validates the --example-task-support flag value and
// turns it into a typed manifest.TaskSupport. An empty value is the zero
// value (the historical "task_support: forbidden" shape — D-164's
// renderer normalises it). An unknown value is a clear CLI-facing error.
func parseExampleTaskSupport(s string) (manifest.TaskSupport, error) {
	switch s {
	case "":
		return "", nil
	case string(manifest.TaskSupportForbidden):
		return manifest.TaskSupportForbidden, nil
	case string(manifest.TaskSupportOptional):
		return manifest.TaskSupportOptional, nil
	case string(manifest.TaskSupportRequired):
		return manifest.TaskSupportRequired, nil
	default:
		return "", errf("invalid --example-task-support %q — use forbidden, optional, or required", s)
	}
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
		return errf("%w — choose another name, remove the directory, or pass --here", err)
	case errors.Is(err, scaffold.ErrFileCollision):
		return errf("%w — move or remove the listed file(s), or scaffold elsewhere", err)
	case errors.Is(err, scaffold.ErrUnknownTemplate):
		return errf("%w — run `dockyard new --help` for the registered set", err)
	default:
		return errf("scaffold failed: %w", err)
	}
}

// printResult writes the post-scaffold summary: the directory and the file
// list. Writes to a CLI output stream go to a buffer or os.Stdout — a write
// error there is not actionable, so it is intentionally not surfaced.
func printResult(w io.Writer, res scaffold.Result) {
	fmt.Fprintf(w, "Created %s (%d files)\n", res.Dir, len(res.Files)) //nolint:errcheck // CLI stdout write
	for _, f := range res.Files {
		fmt.Fprintf(w, "  %s\n", f) //nolint:errcheck // CLI stdout write
	}
}

// printNextSteps writes the post-scaffold next-step commands. When the
// post-scaffold steps ran (ready), the project is already tidied and its
// contracts are generated, so the next steps are just build/serve. When they
// were skipped (--no-postgen) or did not complete, the manual `go mod tidy` +
// `dockyard generate` setup is listed first.
func printNextSteps(w io.Writer, dir string, ready bool) {
	fmt.Fprint(w, "\nNext steps:\n") //nolint:errcheck // CLI stdout write
	fmt.Fprintf(w, "  cd %s\n", dir) //nolint:errcheck // CLI stdout write
	if !ready {
		fmt.Fprint(w, "  go mod tidy        # resolve dependencies\n")        //nolint:errcheck // CLI stdout write
		fmt.Fprint(w, "  dockyard generate  # generate contract artifacts\n") //nolint:errcheck // CLI stdout write
	}
	fmt.Fprint(w, "  go test ./...      # run the contract tests\n") //nolint:errcheck // CLI stdout write
	fmt.Fprint(w, "  go run .           # serve over stdio\n")       //nolint:errcheck // CLI stdout write
}

// goModTidyFn runs `go mod tidy` in the project dir. It is a package var so
// tests can drive the success and failure paths without a real toolchain or
// network. The shell-out matches the established pattern (the integration
// tests and the codegen pipeline both invoke the pinned `go` toolchain).
var goModTidyFn = func(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return err
	}
	return nil
}

// generateFn loads the scaffolded project's manifest and runs the codegen
// pipeline in-process (the same RFC §6.2 path `dockyard generate` runs). It is
// a package var so tests can drive it without a real project on disk.
var generateFn = func(dir string) error {
	m, err := manifest.LoadFile(filepath.Join(dir, manifest.DefaultFilename))
	if err != nil {
		return err
	}
	_, err = generate.Run(generate.Options{ProjectDir: dir, Manifest: m})
	return err
}

// runPostScaffold runs the two one-time steps a fresh scaffold needs to reach a
// green `dockyard validate`: `go mod tidy` (resolve the replace-directive deps
// the generated go.mod declares — RFC §4.3) then `dockyard generate`
// (materialise a template's JSON Schema + TypeScript — RFC §6.2). It returns
// true when both completed.
//
// Both steps are best-effort: a failure (typically no network to the module
// proxy) prints a single warning and returns false, leaving printNextSteps to
// show the manual fallback. The project tree is already written, so a failed
// post-step never fails the scaffold (D-166).
func runPostScaffold(ctx context.Context, out, errOut io.Writer, dir string) bool {
	fmt.Fprint(out, "\nResolving dependencies (go mod tidy)...\n") //nolint:errcheck // CLI stdout write
	if err := goModTidyFn(ctx, dir); err != nil {
		warnPostScaffold(errOut, "go mod tidy", err)
		return false
	}
	fmt.Fprint(out, "Generating contracts (dockyard generate)...\n") //nolint:errcheck // CLI stdout write
	if err := generateFn(dir); err != nil {
		warnPostScaffold(errOut, "dockyard generate", err)
		return false
	}
	return true
}

// warnPostScaffold reports a best-effort post-step that did not complete. It
// does not list the manual recovery commands — printNextSteps shows those
// uniformly (so the developer sees the recovery steps in exactly one place,
// whether the steps were skipped or failed).
func warnPostScaffold(errOut io.Writer, step string, err error) {
	fmt.Fprintf(errOut, "\nwarning: %s did not complete (%s).\n", step, firstLine(err.Error())) //nolint:errcheck // CLI stderr write
	fmt.Fprint(errOut, "The project was created — finish setup with the steps below.\n")        //nolint:errcheck // CLI stderr write
}

// firstLine returns the first line of s, trimmed — the actionable head of a
// toolchain error (the full multi-line dump is noise in a CLI warning).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
