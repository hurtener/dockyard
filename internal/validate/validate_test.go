package validate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Report / Severity -------------------------------------------------------

func TestReport_HasBlockers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ds   []Diagnostic
		want bool
	}{
		{"empty", nil, false},
		{"warning only", []Diagnostic{{Severity: Warning}}, false},
		{"one blocker", []Diagnostic{{Severity: Warning}, {Severity: Blocker}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Report{Diagnostics: tt.ds}
			if got := r.HasBlockers(); got != tt.want {
				t.Errorf("HasBlockers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReport_BlockersAndWarnings(t *testing.T) {
	t.Parallel()
	r := &Report{Diagnostics: []Diagnostic{
		{Check: CheckManifest, Severity: Blocker},
		{Check: CheckSpec, Severity: Warning},
		{Check: CheckSchema, Severity: Blocker},
	}}
	if got := len(r.Blockers()); got != 2 {
		t.Errorf("Blockers() count = %d, want 2", got)
	}
	if got := len(r.Warnings()); got != 1 {
		t.Errorf("Warnings() count = %d, want 1", got)
	}
}

func TestSeverityString(t *testing.T) {
	t.Parallel()
	if Blocker.String() != "blocker" || Warning.String() != "warning" {
		t.Errorf("Severity.String mismatch: %q / %q", Blocker, Warning)
	}
}

func TestRun_RequiresProjectDir(t *testing.T) {
	t.Parallel()
	_, err := Run(Options{})
	if !errors.Is(err, ErrValidate) {
		t.Fatalf("Run with no ProjectDir: want ErrValidate, got %v", err)
	}
}

// --- fixture builders --------------------------------------------------------

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

const validManifest = `name: demo
title: Demo
version: 0.1.0
runtime:
  transports: [stdio]
tools:
  - name: greet
    description: Greet someone.
    input: internal/contracts.GreetInput
    output: internal/contracts.GreetOutput
    task_support: forbidden
`

// hasDiagnostic reports whether the report carries a diagnostic of the given
// check and severity.
func hasDiagnostic(r *Report, c Check, s Severity) bool {
	for _, d := range r.Diagnostics {
		if d.Check == c && d.Severity == s {
			return true
		}
	}
	return false
}

// --- manifest check ----------------------------------------------------------

func TestCheckManifest_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no dockyard.app.yaml
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckManifest, Blocker) {
		t.Fatalf("missing manifest must be a CheckManifest Blocker; got %v", report.Diagnostics)
	}
	if !report.HasBlockers() {
		t.Error("missing manifest must make the report have blockers")
	}
}

func TestCheckManifest_Invalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A manifest with a blank required name — does not load.
	writeFile(t, dir, "dockyard.app.yaml", strings.Replace(validManifest, "name: demo", `name: ""`, 1))
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckManifest, Blocker) {
		t.Fatalf("invalid manifest must be a CheckManifest Blocker; got %v", report.Diagnostics)
	}
}

func TestCheckManifest_BrokenToolUIMapping(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A tool wired to a ui id no apps[] entry declares — a manifest fault.
	bad := validManifest + "    ui: ghost\n"
	writeFile(t, dir, "dockyard.app.yaml", bad)
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.HasBlockers() {
		t.Fatalf("a broken tool↔UI mapping must block; got %v", report.Diagnostics)
	}
}

// --- schema check ------------------------------------------------------------

func TestCheckSchemas_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", validManifest)
	// internal/contracts exists (so the stale check can run) but no schema files.
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckSchema, Blocker) {
		t.Fatalf("a missing schema file must be a CheckSchema Blocker; got %v", report.Diagnostics)
	}
}

func TestCheckSchemas_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", validManifest)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	writeFile(t, dir, "internal/contracts/greet_input.schema.json", "{ this is not json")
	writeFile(t, dir, "internal/contracts/greet_output.schema.json", `{"type":"object"}`)
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckSchema, Blocker) {
		t.Fatalf("an unparseable schema must be a CheckSchema Blocker; got %v", report.Diagnostics)
	}
}

func TestCheckSchemas_RejectsExternalReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", validManifest)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	writeFile(t, dir, "internal/contracts/greet_input.schema.json", `{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://example.com/input"}`)
	writeFile(t, dir, "internal/contracts/greet_output.schema.json", `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"string"}`)
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckSchema, Blocker) {
		t.Fatalf("external ref must block; got %v", report.Diagnostics)
	}
}

// --- tool↔UI mapping check ---------------------------------------------------

const manifestWithApp = `name: demo
title: Demo
version: 0.1.0
runtime:
  transports: [stdio]
  ui:
    framework: svelte
    bundle: single-file
tools:
  - name: greet
    description: Greet someone.
    input: internal/contracts.GreetInput
    output: internal/contracts.GreetOutput
    ui: greet_card
    task_support: forbidden
apps:
  - id: greet_card
    uri: ui://demo/greet
    entry: web/src/apps/greet.svelte
    display_modes: [inline]
`

func TestCheckToolUIMappings_MissingEntryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", manifestWithApp)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	// No web/src/apps/greet.svelte on disk.
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckMapping, Blocker) {
		t.Fatalf("a missing App entry file must be a CheckMapping Blocker; got %v", report.Diagnostics)
	}
}

func TestCheckToolUIMappings_EntryPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", manifestWithApp)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	writeFile(t, dir, "web/src/apps/greet.svelte", "<script>let loading; let empty; let error;</script>")
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasDiagnostic(report, CheckMapping, Blocker) {
		t.Fatalf("a present .svelte entry must not be a CheckMapping Blocker; got %v", report.Diagnostics)
	}
}

// --- UI states check ---------------------------------------------------------

const manifestUIStateGated = manifestWithApp + `quality:
  require_empty_state: true
  require_error_state: true
`

func TestCheckUIStates_MissingState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", manifestUIStateGated)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	// The component mentions neither "empty" nor "error".
	writeFile(t, dir, "web/src/apps/greet.svelte", "<script>let ready;</script>")
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckUIStates, Blocker) {
		t.Fatalf("a UI-state gate with no matching state must block; got %v", report.Diagnostics)
	}
}

func TestCheckUIStates_StatesPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "dockyard.app.yaml", manifestUIStateGated)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	writeFile(t, dir, "web/src/apps/greet.svelte",
		"<script>let empty; let error;</script>{#if empty}no data{/if}{#if error}retry{/if}")
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasDiagnostic(report, CheckUIStates, Blocker) {
		t.Fatalf("a component with the required states must not block on UI states; got %v", report.Diagnostics)
	}
}

func TestCheckUIStates_NoGateNoCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// manifestWithApp opts in no UI-state gate.
	writeFile(t, dir, "dockyard.app.yaml", manifestWithApp)
	writeFile(t, dir, "internal/contracts/contracts.go", "package contracts\n")
	writeFile(t, dir, "web/src/apps/greet.svelte", "<script>let ready;</script>")
	report, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasDiagnostic(report, CheckUIStates, Blocker) {
		t.Fatalf("no UI-state gate opted in ⇒ no UI-state diagnostic; got %v", report.Diagnostics)
	}
}

// --- diagnostic ordering -----------------------------------------------------

func TestSortDiagnostics_StableCheckOrder(t *testing.T) {
	t.Parallel()
	ds := []Diagnostic{
		{Check: CheckStaleCodegen, Severity: Blocker, Message: "z"},
		{Check: CheckManifest, Severity: Blocker, Message: "a"},
		{Check: CheckSchema, Severity: Warning, Message: "m"},
	}
	sortDiagnostics(ds)
	if ds[0].Check != CheckManifest || ds[1].Check != CheckSchema || ds[2].Check != CheckStaleCodegen {
		t.Errorf("diagnostics not in check order: %v", ds)
	}
}
