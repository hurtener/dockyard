package validate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// repoRoot returns the Dockyard repository root, three directories up from this
// test file (internal/validate/<file>).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test file path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// scaffoldAndGenerate scaffolds a real project, `go mod tidy`s it, and runs the
// real generate pipeline — the clean, fully-generated project the validate
// happy paths are exercised against.
func scaffoldAndGenerate(t *testing.T, name string) string {
	t.Helper()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             t.TempDir(),
		DockyardReplace: repoRoot(t),
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	m, err := manifest.LoadFile(filepath.Join(res.Dir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if _, err := generate.Run(generate.Options{ProjectDir: res.Dir, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	return res.Dir
}

// TestRun_CleanProjectHasNoBlockers runs the full validate gate against a real,
// freshly generated project and asserts every check passes — exercising the
// happy path of the schema, MIME, spec-compliance and stale-codegen checks.
func TestRun_CleanProjectHasNoBlockers(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldAndGenerate(t, "val-clean-unit")

	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.HasBlockers() {
		var b strings.Builder
		for _, d := range report.Blockers() {
			b.WriteString("\n  ")
			b.WriteString(d.String())
		}
		t.Fatalf("a clean generated project must have no build blockers; got:%s", b.String())
	}
}

func TestStaleCodegenRejectsOtherContractPackage(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "validate-other-contract-package")
	otherDir := filepath.Join(projectDir, "internal", "other")
	if err := os.MkdirAll(otherDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "contracts.go"), []byte("package other\ntype Input struct { Value string `json:\"value\"` }\ntype Output struct { Result string `json:\"result\"` }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools, manifest.Tool{
		Name: "other", Description: "other", Input: "internal/other.Input", Output: "internal/other.Output", TaskSupport: manifest.TaskSupportForbidden,
	})
	rp := &reporter{}
	lm := loadedManifest{m: m}
	checkStaleCodegen(rp, projectDir, lm)
	if len(rp.diagnostics) != 1 || !strings.Contains(rp.diagnostics[0].Message, `canonical package "internal/contracts"`) {
		t.Fatalf("noncanonical contract package diagnostics = %v", rp.diagnostics)
	}
}

// TestRun_StaleCodegenIsBlocker mutates a contract after generation and asserts
// the stale-codegen check fires — the P1 enforcement, in-package.
func TestRun_StaleCodegenIsBlocker(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldAndGenerate(t, "val-stale-unit")

	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contractsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	drift := string(src) + "\n// Drift.\ntype Drift struct {\n\tX string `json:\"x\"`\n}\n"
	if err := os.WriteFile(contractsPath, []byte(drift), 0o600); err != nil { //nolint:gosec // contractsPath is under a test temp dir
		t.Fatalf("write drift: %v", err)
	}

	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("stale generated output must be a CheckStaleCodegen Blocker; got %v", report.Diagnostics)
	}
}

func TestRun_CustomContractJSONEncodingIsBlocker(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-custom-encoding-unit")
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	f, err := os.OpenFile(contractsPath, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("\nfunc (*GreetOutput) MarshalText() ([]byte, error) { return nil, nil }\n")
	if closeErr := f.Close(); writeErr != nil || closeErr != nil {
		t.Fatalf("append custom encoder: %v / %v", writeErr, closeErr)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("custom contract encoding must block validation: %v", report.Diagnostics)
	}
	found := false
	for _, diagnostic := range report.Blockers() {
		found = found || diagnostic.Check == CheckStaleCodegen && strings.Contains(diagnostic.Message, "custom JSON encoding")
	}
	if !found {
		t.Fatalf("custom encoding diagnostic missing: %v", report.Diagnostics)
	}
}

func TestRun_UnownedSchemaIsNotTreatedAsGenerated(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-unowned-unit")
	unowned := filepath.Join(projectDir, filepath.FromSlash(generate.SchemaFileName("manual", "input")))
	if err := os.WriteFile(unowned, []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("an unowned matching schema must not be treated as generated: %v", report.Diagnostics)
	}
}

func TestRun_MissingOwnershipIndexIsBlocker(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-missing-owner-unit")
	if err := os.Remove(filepath.Join(projectDir, ".dockyard", "generated-artifacts.json")); err != nil {
		t.Fatal(err)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("missing generated ownership index must be stale: %v", report.Diagnostics)
	}
}

func TestRun_IncompleteOwnershipIndexIsBlocker(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-incomplete-owner-unit")
	index := filepath.Join(projectDir, ".dockyard", "generated-artifacts.json")
	if err := os.WriteFile(index, []byte("{\n  \"version\": 1,\n  \"artifacts\": []\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("incomplete generated ownership index must be stale: %v", report.Diagnostics)
	}
}

func TestRun_NoncanonicalOwnershipIndexIsBlocker(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-noncanonical-owner-unit")
	index := filepath.Join(projectDir, ".dockyard", "generated-artifacts.json")
	raw, err := os.ReadFile(index) //nolint:gosec // index is inside the test's scaffolded temporary project.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, bytes.ReplaceAll(raw, []byte("  "), []byte("    ")), 0o600); err != nil { //nolint:gosec // index is inside the test's scaffolded temporary project.
		t.Fatal(err)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("noncanonical generated ownership index must be stale: %v", report.Diagnostics)
	}
}

func TestRun_ExtraMissingOwnershipRecordIsBlocker(t *testing.T) {
	projectDir := scaffoldAndGenerate(t, "val-extra-owner-unit")
	index := filepath.Join(projectDir, ".dockyard", "generated-artifacts.json")
	raw, err := os.ReadFile(index) //nolint:gosec // index is inside the test's scaffolded temporary project.
	if err != nil {
		t.Fatal(err)
	}
	extra := `{"path":"internal/contracts/obsolete_output.schema.json","sha256":"` + strings.Repeat("0", 64) + `"},`
	raw = []byte(strings.Replace(string(raw), `"artifacts": [`, `"artifacts": [`+extra, 1))
	if err := os.WriteFile(index, raw, 0o600); err != nil { //nolint:gosec // index is inside the test's scaffolded temporary project.
		t.Fatal(err)
	}
	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, CheckStaleCodegen, Blocker) {
		t.Fatalf("extra missing ownership record must be stale: %v", report.Diagnostics)
	}
}

// TestRun_CrossCheckDriftIsBlocker proves `dockyard validate` runs
// codegen.CrossCheck (D-113): it mutates the committed contracts.ts so the
// TypeScript and the committed JSON Schema for a tool contract no longer agree,
// then asserts validate reports a schema↔TS drift Blocker. Without CrossCheck
// wired into validate, an internally inconsistent committed pair would pass
// `dockyard validate` — and therefore `dockyard build`, which runs validate.
func TestRun_CrossCheckDriftIsBlocker(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldAndGenerate(t, "val-crosscheck-unit")

	tsPath := filepath.Join(projectDir, filepath.FromSlash(generate.TSFileName()))
	tsRaw, err := os.ReadFile(tsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.ts: %v", err)
	}
	// Inject an extra property into the first generated interface: it is now
	// present in the TypeScript but absent from the schema — exactly the
	// schema↔TS desync CrossCheck exists to catch.
	ts := string(tsRaw)
	open := strings.Index(ts, "{")
	if open < 0 {
		t.Fatalf("no interface body found in contracts.ts:\n%s", ts)
	}
	drifted := ts[:open+1] + "\n  injectedDrift: string;" + ts[open+1:]
	if err := os.WriteFile(tsPath, []byte(drifted), 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write drifted contracts.ts: %v", err)
	}

	report, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, d := range report.Blockers() {
		if d.Check == CheckStaleCodegen && strings.Contains(d.Message, "drifted apart") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an internally inconsistent schema/TS pair must produce a "+
			"schema↔TS drift Blocker; got %v", report.Diagnostics)
	}
}

// TestDiagnosticString covers the Diagnostic and Check string rendering.
func TestDiagnosticString(t *testing.T) {
	t.Parallel()
	d := Diagnostic{Check: CheckSchema, Severity: Blocker, Message: "bad schema"}
	got := d.String()
	for _, want := range []string{"blocker", "schema", "bad schema"} {
		if !strings.Contains(got, want) {
			t.Errorf("Diagnostic.String() = %q, missing %q", got, want)
		}
	}
}

// TestReporterWarn covers the warning path of the reporter.
func TestReporterWarn(t *testing.T) {
	t.Parallel()
	rp := &reporter{}
	rp.warn(CheckSpec, "a soft signal about %s", "tasks")
	if len(rp.diagnostics) != 1 || rp.diagnostics[0].Severity != Warning {
		t.Fatalf("warn did not record a Warning diagnostic: %v", rp.diagnostics)
	}
	if !strings.Contains(rp.diagnostics[0].Message, "tasks") {
		t.Errorf("warn message not formatted: %q", rp.diagnostics[0].Message)
	}
}

// TestRun_SpecComplianceGateRespectsFlag proves the require_spec_compliance
// quality gate is enforced (D-175) — previously it was declared-but-dead: the
// spec check ran unconditionally and toggling the flag changed nothing (the
// D-168 class). A docs/specifications/ tree with a withheld spec makes the
// spec check a Blocker when the flag is on; flipping it off opts out.
func TestRun_SpecComplianceGateRespectsFlag(t *testing.T) {
	t.Parallel()
	dir := scaffoldAndGenerate(t, "val-spec-gate")

	// Induce the spec-absence Blocker: a docs/specifications/ tree present but
	// missing one vendored spec (the exact regression checkSpecCompliance guards).
	specsDir := filepath.Join(dir, "docs", "specifications")
	if err := os.MkdirAll(specsDir, 0o750); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "mcp-apps-2026-01-26.mdx"),
		[]byte("vendored spec snapshot\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// Flag on (the scaffold default) → the withheld spec is a CheckSpec Blocker.
	on, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run (flag on): %v", err)
	}
	if !hasDiagnostic(on, CheckSpec, Blocker) {
		t.Fatalf("require_spec_compliance: true must enforce the spec check; got %v", on.Diagnostics)
	}

	// Flip require_spec_compliance → false: the gate opts out, no CheckSpec
	// diagnostic. Before D-175 this flag was inert and the Blocker still fired.
	manifestPath := filepath.Join(dir, "dockyard.app.yaml")
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	flipped := strings.Replace(string(raw),
		"require_spec_compliance: true", "require_spec_compliance: false", 1)
	if flipped == string(raw) {
		t.Fatal("scaffold manifest did not contain require_spec_compliance: true to flip")
	}
	if err := os.WriteFile(manifestPath, []byte(flipped), 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write manifest: %v", err)
	}

	off, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run (flag off): %v", err)
	}
	if hasDiagnostic(off, CheckSpec, Blocker) {
		t.Fatalf("require_spec_compliance: false must skip the spec check (D-175); got %v", off.Diagnostics)
	}
}
