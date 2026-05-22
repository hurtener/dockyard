package validate

import (
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
