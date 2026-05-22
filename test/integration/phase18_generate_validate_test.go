// This file is the Phase 18 integration test (CLAUDE.md §17). Phase 18's deps
// name shipped phases 17/05/09/13 and it consumes internal/codegen,
// internal/manifest and runtime/apps, so it ships an end-to-end integration
// test driven against real components with no mocks at the seam:
//
//   - it runs the real `dockyard new` scaffold to produce a project;
//   - it `go mod tidy`s it against the real Dockyard checkout (replace
//     directive), so the ephemeral schema generator's `go run` resolves;
//   - it runs the real internal/generate.Run pipeline and asserts a second run
//     is a byte-identical no-op (the idempotency acceptance criterion);
//   - it runs the real internal/validate.Run gate across a clean project
//     (no blockers) AND each build-blocker class — an invalid manifest, a
//     broken tool↔UI mapping, and stale generated output — asserting a Blocker
//     each time (the non-zero-exit acceptance criterion).
//
// The stale-drift failure mode is covered explicitly. The test runs under -race.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/internal/validate"
)

// scaffoldP18Project runs the real scaffold and `go mod tidy`, returning the
// project directory. The project's go.mod replaces the Dockyard import at this
// repo's root so the ephemeral schema generator compiles against the real
// runtime library.
func scaffoldP18Project(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	parent := t.TempDir()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             parent,
		DockyardReplace: root,
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in scaffolded project failed: %v\n%s", err, out)
	}
	return res.Dir
}

// loadP18Manifest loads the project's manifest or fails the test.
func loadP18Manifest(t *testing.T, projectDir string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

// snapshotContracts reads every generated artifact under internal/contracts.
func snapshotContracts(t *testing.T, projectDir string) map[string][]byte {
	t.Helper()
	dir := filepath.Join(projectDir, "internal", "contracts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read contracts dir: %v", err)
	}
	snap := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		snap[e.Name()] = raw
	}
	return snap
}

// TestPhase18_GenerateIsIdempotent runs the real generate pipeline twice and
// asserts the second run is a byte-identical no-op — the binding idempotency
// acceptance criterion (master plan Phase 18, RFC §6.2).
func TestPhase18_GenerateIsIdempotent(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP18Project(t, "gen-idem")
	m := loadP18Manifest(t, projectDir)

	// First generate — produces the artifacts.
	res1, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("first generate.Run: %v", err)
	}
	if len(res1.Written) == 0 {
		t.Fatal("first generate wrote no files")
	}
	before := snapshotContracts(t, projectDir)

	// Second generate — must change nothing.
	res2, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("second generate.Run: %v", err)
	}
	if len(res2.Changed) != 0 {
		t.Errorf("second generate reported %d changed files, want 0 (not idempotent): %v",
			len(res2.Changed), res2.Changed)
	}
	after := snapshotContracts(t, projectDir)

	if len(before) != len(after) {
		t.Fatalf("contract file count changed across reruns: %d → %d", len(before), len(after))
	}
	for name, b := range before {
		a, ok := after[name]
		if !ok {
			t.Errorf("file %s disappeared after a second generate", name)
			continue
		}
		if string(a) != string(b) {
			t.Errorf("file %s is not byte-identical after a second generate — generate is not idempotent", name)
		}
	}
}

// TestPhase18_ValidatePassesCleanProject runs the real validate gate against a
// freshly scaffolded, freshly generated project and asserts no build blockers.
func TestPhase18_ValidatePassesCleanProject(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP18Project(t, "val-clean")
	m := loadP18Manifest(t, projectDir)
	if _, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("validate.Run: %v", err)
	}
	if report.HasBlockers() {
		t.Fatalf("a clean scaffolded project must not have build blockers; got:\n%s",
			renderDiagnostics(report))
	}
}

// TestPhase18_ValidateBlocksInvalidManifest drives the manifest build-blocker
// class: a manifest missing a required field must produce a Blocker.
func TestPhase18_ValidateBlocksInvalidManifest(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP18Project(t, "val-badman")

	manifestPath := filepath.Join(projectDir, manifest.DefaultFilename)
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	// Blank the required `name` field — the manifest no longer loads.
	broken := strings.Replace(string(raw), "name: val-badman", `name: ""`, 1)
	if broken == string(raw) {
		t.Fatal("manifest fixture mutation did not apply")
	}
	if err := os.WriteFile(manifestPath, []byte(broken), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write broken manifest: %v", err)
	}

	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("validate.Run: %v", err)
	}
	if !report.HasBlockers() {
		t.Fatal("an invalid manifest must produce a build blocker")
	}
	if !hasCheck(report, validate.CheckManifest) {
		t.Errorf("expected a CheckManifest blocker; got:\n%s", renderDiagnostics(report))
	}
}

// TestPhase18_ValidateBlocksBrokenToolUIMapping drives the tool↔UI mapping
// build-blocker class: a tool wired to a ui id no apps[] entry declares.
func TestPhase18_ValidateBlocksBrokenToolUIMapping(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP18Project(t, "val-badmap")

	manifestPath := filepath.Join(projectDir, manifest.DefaultFilename)
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	// Add a tools[].ui that references an app id no apps[] entry declares.
	broken := strings.Replace(string(raw),
		"output: internal/contracts.GreetOutput",
		"output: internal/contracts.GreetOutput\n    ui: ghost", 1)
	if broken == string(raw) {
		t.Fatal("manifest fixture mutation did not apply")
	}
	if err := os.WriteFile(manifestPath, []byte(broken), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write broken manifest: %v", err)
	}

	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("validate.Run: %v", err)
	}
	if !report.HasBlockers() {
		t.Fatal("a broken tool↔UI mapping must produce a build blocker")
	}
}

// TestPhase18_ValidateBlocksStaleCodegen drives the stale-codegen build-blocker
// class — the P1 enforcement (RFC §6.2). It generates a clean project, then
// edits a contract struct without rerunning generate, and asserts validate
// flags the now-stale generated output as a Blocker.
func TestPhase18_ValidateBlocksStaleCodegen(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP18Project(t, "val-stale")
	m := loadP18Manifest(t, projectDir)
	if _, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	// Sanity: a freshly generated project has no stale-codegen blocker.
	clean, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("validate.Run (clean): %v", err)
	}
	if hasCheck(clean, validate.CheckStaleCodegen) {
		t.Fatalf("a freshly generated project must not be stale; got:\n%s", renderDiagnostics(clean))
	}

	// Mutate a contract struct without rerunning generate — the committed
	// schema/TS are now stale versus the Go source.
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contractsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	drift := string(src) + "\n// Drift forces the stale-codegen check to fire.\n" +
		"type Drift struct {\n\tX string `json:\"x\"`\n}\n"
	if err := os.WriteFile(contractsPath, []byte(drift), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write drifted contracts.go: %v", err)
	}

	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("validate.Run (stale): %v", err)
	}
	if !report.HasBlockers() {
		t.Fatal("stale generated output must produce a build blocker")
	}
	if !hasCheck(report, validate.CheckStaleCodegen) {
		t.Errorf("expected a CheckStaleCodegen blocker; got:\n%s", renderDiagnostics(report))
	}
}

// hasCheck reports whether the report carries a Blocker of the given check.
func hasCheck(r *validate.Report, c validate.Check) bool {
	for _, d := range r.Blockers() {
		if d.Check == c {
			return true
		}
	}
	return false
}

// renderDiagnostics formats a report's diagnostics for a test failure message.
func renderDiagnostics(r *validate.Report) string {
	var b strings.Builder
	for _, d := range r.Diagnostics {
		b.WriteString("  ")
		b.WriteString(d.String())
		b.WriteString("\n")
	}
	return b.String()
}
