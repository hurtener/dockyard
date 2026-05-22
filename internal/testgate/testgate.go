package testgate

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/hurtener/dockyard/internal/manifest"
)

// ErrTestGate is the sentinel wrapping a failure that is not itself a category
// regression — a missing project, an unloadable manifest, an I/O fault. A
// category regression is never a returned error: it is a failed Result in the
// Report. Run returns a nil error and a Report carrying the verdicts. Callers
// branch with errors.Is(err, ErrTestGate).
var ErrTestGate = errors.New("dockyard/internal/testgate: the test gate could not run")

// Category names one test category `dockyard test` runs.
type Category string

const (
	// CategoryGoTest is the project's own Go unit tests (`go test ./...`).
	CategoryGoTest Category = "go-test"
	// CategoryContract is the contract-first assertion: the generated schema
	// and TypeScript still match the Go contract structs (P1).
	CategoryContract Category = "contract"
	// CategoryGolden is the project's fixture / golden snapshot check.
	CategoryGolden Category = "golden"
	// CategorySpecCompliance is MCP spec compliance against the vendored specs.
	CategorySpecCompliance Category = "spec-compliance"
	// CategoryCapability is the capability-degradation category: the project
	// degrades gracefully across host capability sets (RFC §7.5).
	CategoryCapability Category = "capability"
)

// categoryOrder pins the display and execution order of the categories so a
// Report reads top-down in a stable, predictable order.
var categoryOrder = []Category{
	CategoryGoTest,
	CategoryContract,
	CategoryGolden,
	CategorySpecCompliance,
	CategoryCapability,
}

// Result is one category's verdict.
type Result struct {
	// Category is which category produced this Result.
	Category Category
	// Passed reports whether the category found no regression.
	Passed bool
	// Gating reports whether a failure of this category exits the process
	// non-zero. Every V1 category is gating (RFC §9.4 — these are build
	// blockers); the field is explicit so a future informational category can
	// be added without changing the exit-code logic.
	Gating bool
	// Detail is the human-facing, actionable description: on a pass, a one-line
	// summary; on a fail, the specific regression and how to fix it.
	Detail string
}

// String renders a Result as one actionable line.
func (r Result) String() string {
	verdict := "PASS"
	if !r.Passed {
		verdict = "FAIL"
	}
	return fmt.Sprintf("[%s] %s: %s", verdict, r.Category, r.Detail)
}

// Report is the structured outcome of a test-gate run: every category's
// Result, in category order.
type Report struct {
	// Results is one entry per category that ran, in categoryOrder.
	Results []Result
}

// Failed reports whether any gating category failed — the exit-code seam:
// `dockyard test` exits non-zero exactly when this is true. A failed
// non-gating (informational) category never flips it.
func (r *Report) Failed() bool {
	for _, res := range r.Results {
		if res.Gating && !res.Passed {
			return true
		}
	}
	return false
}

// Passed reports whether every gating category passed — the inverse of Failed.
func (r *Report) Passed() bool { return !r.Failed() }

// Options configures one test-gate run.
type Options struct {
	// ProjectDir is the root of the Dockyard project — the directory holding
	// dockyard.app.yaml. Required.
	ProjectDir string
	// SkipGoTest skips the go-test category. `go test` is the slowest category;
	// a fast non-interactive run (a smoke script) skips it while still
	// exercising the contract, golden, spec, and capability gates. A real
	// `dockyard test` invocation never sets it.
	SkipGoTest bool
}

// Run executes every test category against the Dockyard project rooted at
// opts.ProjectDir and returns a Report.
//
// A non-nil error means the gate could not run at all (a missing project, an
// unloadable manifest) — it wraps ErrTestGate. A category regression is never a
// returned error: it is a failed Result in the Report. The caller decides the
// exit code from Report.Failed.
//
// Run composes the existing seams — internal/validate.Run, internal/generate,
// internal/codegen, runtime/apps — it does not reimplement them. It builds
// fresh state per call and holds no shared mutable state, so it is safe to
// invoke concurrently.
func Run(opts Options) (*Report, error) {
	if opts.ProjectDir == "" {
		return nil, fmt.Errorf("%w: ProjectDir is required", ErrTestGate)
	}

	// The manifest is load-bearing for every category: a project whose manifest
	// will not load at all has nothing coherent to test against. That is a
	// run-fault (ErrTestGate), not a category regression.
	m, err := loadManifest(opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	rep := &Report{}
	for _, cat := range categoryOrder {
		if cat == CategoryGoTest && opts.SkipGoTest {
			continue
		}
		rep.Results = append(rep.Results, runCategory(cat, opts.ProjectDir, m))
	}
	return rep, nil
}

// runCategory dispatches one category to its runner.
func runCategory(cat Category, projectDir string, m *manifest.Manifest) Result {
	switch cat {
	case CategoryGoTest:
		return runGoTest(projectDir)
	case CategoryContract:
		return runContract(projectDir, m)
	case CategoryGolden:
		return runGolden(projectDir, m)
	case CategorySpecCompliance:
		return runSpecCompliance(projectDir)
	case CategoryCapability:
		return runCapability(projectDir, m)
	default:
		// Unreachable: categoryOrder is the closed set. A defensive Result is
		// returned rather than a panic — never panic for control flow.
		return Result{Category: cat, Passed: false, Gating: true,
			Detail: "internal error: unknown test category"}
	}
}

// loadManifest loads and structurally validates the project's
// dockyard.app.yaml. A manifest that will not load is a run-fault wrapping
// ErrTestGate — there is nothing coherent to test.
func loadManifest(projectDir string) (*manifest.Manifest, error) {
	path := filepath.Join(projectDir, manifest.DefaultFilename)
	m, err := manifest.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w — is %s a Dockyard project?",
			ErrTestGate, err, projectDir)
	}
	return m, nil
}
