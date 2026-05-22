package inspector

import (
	"github.com/hurtener/dockyard/internal/validate"
)

// Verdict is one row of the inspector's Verdicts panel — a contract-drift,
// schema-validation, or spec-compliance result surfaced read-only to the
// inspector UI as an ok / warn / error chip (RFC §12). It is the inspector's
// own type: no raw validate or codegen struct leaks through it (P3).
type Verdict struct {
	// Check is the verdict's check class: "manifest", "schema",
	// "spec-compliance", "stale-codegen", … — the validate Check taxonomy.
	Check string `json:"check"`
	// Severity is the rendered tone: "ok" | "warn" | "error".
	Severity string `json:"severity"`
	// Message is the human-facing, actionable description.
	Message string `json:"message"`
}

// Verdict severity strings — the values the inspector UI maps onto a StatusChip
// tone. They are NOT the validate.Severity enum: a passing check has no
// validate Diagnostic, so the inspector synthesises an "ok" verdict itself.
const (
	verdictOK    = "ok"
	verdictWarn  = "warn"
	verdictError = "error"
)

// VerdictSource produces the current verdict set on demand. The inspector calls
// it per `GET /api/verdicts` request so the verdicts reflect the project as it
// is now — re-running the checks, never caching a stale result.
type VerdictSource func() []Verdict

// VerdictsFromValidate adapts internal/validate.Run into a [VerdictSource]
// rooted at projectDir. It is the seam RFC §12 names: the Verdicts panel reuses
// the `dockyard validate` engine — contract-drift (stale-codegen), schema
// validation, and spec compliance — rather than reimplementing the checks.
//
// The returned source maps every validate Diagnostic to a Verdict row. When a
// run produces no diagnostics at all, it returns a single "ok" verdict so the
// panel renders a clean ready state rather than an empty one. When validation
// cannot run at all (a missing project), it returns one "error" verdict
// describing the fault — the panel degrades gracefully, never blank.
func VerdictsFromValidate(projectDir string) VerdictSource {
	return func() []Verdict {
		report, err := validate.Run(validate.Options{ProjectDir: projectDir})
		if err != nil {
			return []Verdict{{
				Check:    "validate",
				Severity: verdictError,
				Message:  "verdicts unavailable: " + err.Error(),
			}}
		}
		return verdictsFromReport(report)
	}
}

// verdictsFromReport maps a validate Report onto verdict rows. A report with no
// diagnostics yields one synthesised "ok" verdict (RFC §12 — the panel always
// renders a verdict, not a void).
func verdictsFromReport(report *validate.Report) []Verdict {
	if report == nil || len(report.Diagnostics) == 0 {
		return []Verdict{{
			Check:    "validate",
			Severity: verdictOK,
			Message:  "all quality gates pass — no contract drift, schema, or spec issue",
		}}
	}
	out := make([]Verdict, 0, len(report.Diagnostics))
	for _, d := range report.Diagnostics {
		out = append(out, Verdict{
			Check:    string(d.Check),
			Severity: severityString(d.Severity),
			Message:  d.Message,
		})
	}
	return out
}

// severityString maps a validate.Severity onto the verdict severity string the
// inspector UI renders. A Blocker is an "error" chip; a Warning is a "warn"
// chip.
func severityString(s validate.Severity) string {
	switch s {
	case validate.Blocker:
		return verdictError
	case validate.Warning:
		return verdictWarn
	default:
		return verdictWarn
	}
}
