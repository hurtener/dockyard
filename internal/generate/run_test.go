package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// repoRoot returns the Dockyard repository root, three directories up from this
// test file (internal/generate/<file>). A scaffolded project `replace`s the
// Dockyard import at this path so the ephemeral schema generator's `go run`
// resolves against the real runtime library.
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
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// scaffoldProject runs the real scaffold and `go mod tidy`, returning the
// project directory — the canonical input for the full generate pipeline.
func scaffoldProject(t *testing.T, name string) string {
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
	return res.Dir
}

// TestRun_EndToEnd exercises the full Run pipeline — TypeScript in-process plus
// the ephemeral schema generator `go run` — against a real scaffolded project,
// and asserts the generated files are produced and a rerun is idempotent.
func TestRun_EndToEnd(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "gen-e2e")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The pipeline produces the TypeScript file and both schema files.
	for _, want := range []string{
		TSFileName(),
		SchemaFileName("greet", "input"),
		SchemaFileName("greet", "output"),
	} {
		full := filepath.Join(projectDir, filepath.FromSlash(want))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected generated file %s: %v", want, err)
		}
		found := false
		for _, w := range res.Written {
			if w == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Result.Written does not list %s: %v", want, res.Written)
		}
	}

	// A second run changes nothing — the idempotency guarantee.
	res2, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(res2.Changed) != 0 {
		t.Errorf("second Run changed %d files, want 0 — not idempotent: %v",
			len(res2.Changed), res2.Changed)
	}
}

// TestRun_RegeneratesAfterContractChange proves Run picks up a contract-source
// change: after editing a contract struct, the regenerated artifacts differ.
func TestRun_RegeneratesAfterContractChange(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "gen-change")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Add a field to GreetOutput — the generated schema/TS must now change.
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contractsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	mutated := injectField(string(src))
	if mutated == string(src) {
		t.Fatal("contract mutation did not apply")
	}
	if err := os.WriteFile(contractsPath, []byte(mutated), 0o600); err != nil { //nolint:gosec // contractsPath is under a test temp dir
		t.Fatalf("write mutated contracts.go: %v", err)
	}

	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Run after contract change: %v", err)
	}
	if len(res.Changed) == 0 {
		t.Error("Run after a contract change reported no changed files")
	}
}

// injectField adds an extra field to the GreetOutput struct in scaffolded
// contract source.
func injectField(src string) string {
	const anchor = "type GreetOutput struct {"
	const inject = anchor + "\n\t// Extra is a field added to force regeneration.\n\tExtra string `json:\"extra,omitempty\"`"
	return strings.Replace(src, anchor, inject, 1)
}
