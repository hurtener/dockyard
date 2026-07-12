package testgate

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

// This file exercises the test-gate category runners against a REAL scaffolded
// project — the same in-package "scaffold + go mod tidy" pattern Phase 18's
// internal/generate and internal/validate run-tests use. It is the in-package
// counterpart to the end-to-end test/integration/phase21_test_gate_test.go: it
// covers the happy paths and the regression paths of the category runners that
// need a buildable Go module (runGoTest, runContract, runGolden, runSpec).

// repoRoot returns the Dockyard repository root — three directories up from
// this test file (internal/testgate/<file>).
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

// scaffoldRealProject scaffolds a project, `go mod tidy`s it against the real
// Dockyard checkout, and runs the real generate pipeline — the clean,
// fully-generated project the gate happy paths run against.
func scaffoldRealProject(t *testing.T, name string) string {
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
	m := loadProjectManifest(t, res.Dir)
	if _, err := generate.Run(generate.Options{ProjectDir: res.Dir, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	return res.Dir
}

func loadProjectManifest(t *testing.T, projectDir string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

// TestRun_CleanProjectPassesEveryCategory runs the full gate against a real,
// freshly-scaffolded project and asserts every category passes.
func TestRun_CleanProjectPassesEveryCategory(t *testing.T) {
	t.Parallel()
	dir := scaffoldRealProject(t, "clean-server")

	rep, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("clean project failed the gate:\n%s", renderReport(rep))
	}
	if got := len(rep.Results); got != len(categoryOrder) {
		t.Errorf("clean run has %d categories, want %d", got, len(categoryOrder))
	}
	for _, res := range rep.Results {
		if !res.Passed {
			t.Errorf("category %q failed on a clean project: %s", res.Category, res.Detail)
		}
	}
}

// TestRun_ContractRegressionFailsTheGate edits a contract struct without
// regenerating — the committed schema/TS is now stale. The contract category
// must fail and the gate must fail.
func TestRun_ContractRegressionFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldRealProject(t, "contract-drift")

	// Append a field to a contract struct without rerunning generate.
	contracts := filepath.Join(dir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contracts) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	drift := string(src) + "\n// Drift: a new contract type with no regenerated schema.\n" +
		"type Drift struct {\n\tX string `json:\"x\"`\n}\n"
	if err := os.WriteFile(contracts, []byte(drift), 0o600); err != nil { //nolint:gosec // contracts is under a test temp dir
		t.Fatalf("write drifted contracts.go: %v", err)
	}

	rep, err := Run(Options{ProjectDir: dir, SkipGoTest: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Failed() {
		t.Fatalf("a contract regression did not fail the gate:\n%s", renderReport(rep))
	}
	if !categoryFailed(rep, CategoryContract) {
		t.Errorf("the contract category did not fail on a contract regression:\n%s", renderReport(rep))
	}
}

func TestRunGolden_RejectsNonconformantSchema(t *testing.T) {
	t.Parallel()
	dir := scaffoldRealProject(t, "bad-schema")
	m := loadProjectManifest(t, dir)
	path := filepath.Join(dir, filepath.FromSlash(generate.SchemaFileName("greet", "output")))
	bad := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://example.com/output"}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runGolden(dir, m)
	if result.Passed || !strings.Contains(result.Detail, "external $ref") {
		t.Fatalf("result = %#v", result)
	}
}

// TestRun_SpecComplianceViolationFailsTheGate removes a vendored spec from a
// project that carries a docs/specifications/ tree — validate's CheckSpec then
// reports the missing spec as a Blocker, which the spec-compliance category
// surfaces as a gating failure.
func TestRun_SpecComplianceViolationFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldRealProject(t, "spec-violation")

	// checkSpecCompliance only flags an absent vendored spec when the project
	// has a docs/specifications/ tree (so a plain scaffold is not falsely
	// failed). Create the tree but withhold one spec — the exact regression.
	specsDir := filepath.Join(dir, "docs", "specifications")
	if err := os.MkdirAll(specsDir, 0o750); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	// Provide one spec, withhold the other.
	if err := os.WriteFile(filepath.Join(specsDir, "mcp-apps-2026-01-26.mdx"),
		[]byte("vendored spec snapshot\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	rep, err := Run(Options{ProjectDir: dir, SkipGoTest: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Failed() {
		t.Fatalf("a spec-compliance violation did not fail the gate:\n%s", renderReport(rep))
	}
	if !categoryFailed(rep, CategorySpecCompliance) {
		t.Errorf("the spec-compliance category did not fail:\n%s", renderReport(rep))
	}
}

// TestRun_GoTestFailureFailsTheGate breaks a project's own contract test — the
// go-test category must fail.
func TestRun_GoTestFailureFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldRealProject(t, "go-test-fail")

	// Overwrite the contract test with one that always fails.
	failing := "package main\n\nimport \"testing\"\n\n" +
		"func TestAlwaysFails(t *testing.T) { t.Fatal(\"intentional failure\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "greet_test.go"), []byte(failing), 0o600); err != nil {
		t.Fatalf("write failing test: %v", err)
	}

	rep, err := Run(Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !categoryFailed(rep, CategoryGoTest) {
		t.Errorf("the go-test category did not fail on a failing project test:\n%s", renderReport(rep))
	}
	if !rep.Failed() {
		t.Errorf("a failing project test did not fail the gate")
	}
}

// categoryFailed reports whether the named category is present and failed.
func categoryFailed(r *Report, c Category) bool {
	for _, res := range r.Results {
		if res.Category == c {
			return !res.Passed
		}
	}
	return false
}

// renderReport renders a Report for a test-failure message.
func renderReport(r *Report) string {
	var b strings.Builder
	for _, res := range r.Results {
		b.WriteString("  ")
		b.WriteString(res.String())
		b.WriteString("\n")
	}
	return b.String()
}
