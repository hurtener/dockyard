package validate

import (
	"errors"
	"fmt"
	"sort"

	"github.com/hurtener/dockyard/internal/manifest"
)

// ErrValidate is the sentinel wrapping a validate failure that is not itself a
// quality diagnostic — an I/O fault, a missing project, an internal error. A
// quality fault is a Diagnostic in the Report, never a returned error: Run
// returns a nil error and a Report carrying the faults. Callers branch with
// errors.Is(err, ErrValidate).
var ErrValidate = errors.New("dockyard/internal/validate: validation could not run")

// Severity classifies a Diagnostic. The taxonomy follows RFC §9.4.
type Severity int

const (
	// Blocker is a build blocker (RFC §9.4): an invalid manifest or schema, a
	// missing or mismatched ui:// resource, an invalid MIME, a spec violation,
	// or stale generated contracts. A Report carrying any Blocker forces a
	// non-zero `dockyard validate` exit.
	Blocker Severity = iota
	// Warning is a non-blocking quality signal (RFC §9.4): a UI payload routed
	// into content, a vague description, a missing graceful-degradation path.
	// It is reported but does not change the exit code.
	Warning
)

// String renders a Severity for output.
func (s Severity) String() string {
	switch s {
	case Blocker:
		return "blocker"
	case Warning:
		return "warning"
	default:
		return "unknown"
	}
}

// Check identifies which RFC §9.4 check class produced a Diagnostic.
type Check string

const (
	// CheckManifest — dockyard.app.yaml schema and structural validity.
	CheckManifest Check = "manifest"
	// CheckSchema — the generated JSON Schema artifacts.
	CheckSchema Check = "schema"
	// CheckMapping — tool↔UI resource mappings.
	CheckMapping Check = "tool-ui-mapping"
	// CheckMIME — UI resource MIME types.
	CheckMIME Check = "mime"
	// CheckSpec — MCP spec compliance against the vendored specs.
	CheckSpec Check = "spec-compliance"
	// CheckUIStates — the four-state page rule (CLAUDE.md §20).
	CheckUIStates Check = "ui-states"
	// CheckStaleCodegen — generated output drift from the Go contract structs.
	CheckStaleCodegen Check = "stale-codegen"
)

// Diagnostic is one quality finding.
type Diagnostic struct {
	// Check is the RFC §9.4 class that produced this finding.
	Check Check
	// Severity is Blocker or Warning.
	Severity Severity
	// Message is the human-facing, actionable description.
	Message string
}

// String renders a Diagnostic as one actionable line.
func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s] %s: %s", d.Severity, d.Check, d.Message)
}

// Report is the structured outcome of a validate run: every Diagnostic across
// every check, plus the aggregate verdict. It is the value `dockyard build`
// (Phase 20) and `dockyard test` (Phase 21) consume to gate their own work.
type Report struct {
	// Diagnostics is every finding, in check order then discovery order.
	Diagnostics []Diagnostic
}

// HasBlockers reports whether the Report carries any Blocker diagnostic — the
// exit-code seam: `dockyard validate` exits non-zero exactly when this is true.
func (r *Report) HasBlockers() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == Blocker {
			return true
		}
	}
	return false
}

// Blockers returns only the build-blocker diagnostics.
func (r *Report) Blockers() []Diagnostic { return r.filter(Blocker) }

// Warnings returns only the warning diagnostics.
func (r *Report) Warnings() []Diagnostic { return r.filter(Warning) }

func (r *Report) filter(s Severity) []Diagnostic {
	var out []Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == s {
			out = append(out, d)
		}
	}
	return out
}

// reporter accumulates Diagnostics across the checks.
type reporter struct {
	diagnostics []Diagnostic
}

func (rp *reporter) block(c Check, format string, args ...any) {
	rp.diagnostics = append(rp.diagnostics, Diagnostic{
		Check: c, Severity: Blocker, Message: fmt.Sprintf(format, args...),
	})
}

func (rp *reporter) warn(c Check, format string, args ...any) {
	rp.diagnostics = append(rp.diagnostics, Diagnostic{
		Check: c, Severity: Warning, Message: fmt.Sprintf(format, args...),
	})
}

// Options configures one validate run.
type Options struct {
	// ProjectDir is the root of the Dockyard project — the directory holding
	// dockyard.app.yaml. Required.
	ProjectDir string
}

// Run validates the Dockyard project rooted at opts.ProjectDir against the
// RFC §9.4 quality gates and returns a Report.
//
// A non-nil error means validation could not be performed at all (a missing
// project, an I/O fault) — it wraps ErrValidate. A quality fault is never a
// returned error: it is a Diagnostic in the Report. The caller decides the
// exit code from Report.HasBlockers.
//
// The manifest check runs first and is load-bearing: a manifest that will not
// load at all is reported as a Blocker and the remaining checks that need a
// loaded manifest are skipped (there is nothing coherent to check against).
//
// Run builds fresh state per call and holds no shared mutable state.
func Run(opts Options) (*Report, error) {
	if opts.ProjectDir == "" {
		return nil, fmt.Errorf("%w: ProjectDir is required", ErrValidate)
	}

	rp := &reporter{}

	// --- manifest ----------------------------------------------------------
	m, manifestOK := checkManifest(rp, opts.ProjectDir)

	if manifestOK {
		// The remaining checks all need a loaded manifest.
		checkSchemas(rp, opts.ProjectDir, m)
		checkToolUIMappings(rp, opts.ProjectDir, m)
		checkMIME(rp, m)
		checkSpecCompliance(rp, opts.ProjectDir, m)
		checkUIStates(rp, opts.ProjectDir, m)
		checkStaleCodegen(rp, opts.ProjectDir, m)
	}

	sortDiagnostics(rp.diagnostics)
	return &Report{Diagnostics: rp.diagnostics}, nil
}

// checkOrder pins the display order of the check classes.
var checkOrder = map[Check]int{
	CheckManifest:     0,
	CheckSchema:       1,
	CheckMapping:      2,
	CheckMIME:         3,
	CheckSpec:         4,
	CheckUIStates:     5,
	CheckStaleCodegen: 6,
}

// sortDiagnostics orders diagnostics by check class then severity then message,
// so a Report reads top-down in a stable, predictable order.
func sortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ci, cj := checkOrder[ds[i].Check], checkOrder[ds[j].Check]; ci != cj {
			return ci < cj
		}
		if ds[i].Severity != ds[j].Severity {
			return ds[i].Severity < ds[j].Severity
		}
		return ds[i].Message < ds[j].Message
	})
}

// loadedManifest is the manifest plus the path it was loaded from — passed to
// the checks that need both.
type loadedManifest struct {
	m    *manifest.Manifest
	path string
}
