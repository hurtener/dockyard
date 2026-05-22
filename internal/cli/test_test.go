package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/scaffold"
)

func TestRoot_HelpListsTest(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("root help does not list the 'test' command:\n%s", out)
	}
}

func TestTest_RejectsArgs(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "test", "extra-arg"); err == nil {
		t.Error("test with a positional arg: want an error")
	}
}

func TestTest_FailsWhenNoManifest(t *testing.T) {
	t.Parallel()
	// A directory with no dockyard.app.yaml is not a Dockyard project — the
	// test gate cannot run, surfaced as a non-zero (error) exit.
	dir := t.TempDir()
	_, _, err := run(t, "test", "--dir", dir)
	if err == nil {
		t.Fatal("test in a non-project directory: want an error")
	}
	if !strings.Contains(err.Error(), "test gate could not run") {
		t.Errorf("error should explain the gate could not run: %v", err)
	}
}

func TestTest_HelpDescribesCategories(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "test", "--help")
	if err != nil {
		t.Fatalf("test --help: %v", err)
	}
	for _, want := range []string{"go test", "contract", "spec", "capability"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("test help does not mention %q:\n%s", want, out)
		}
	}
}

// cliRepoRoot returns the Dockyard repo root — two directories up from this
// test file (internal/cli/<file>).
func cliRepoRoot(t *testing.T) string {
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

// TestTest_RunsAgainstCleanProject drives the `dockyard test` command end to
// end against a real scaffolded project — it covers the success path and the
// report printer, exercising the same path a user hits.
func TestTest_RunsAgainstCleanProject(t *testing.T) {
	t.Parallel()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            "cli-test-srv",
		Dir:             t.TempDir(),
		DockyardReplace: cliRepoRoot(t),
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

	// --skip-go-test keeps the CLI test fast; the contract, golden, spec, and
	// capability gates still run — enough to exercise the success path and the
	// report printer.
	out, _, runErr := run(t, "test", "--dir", res.Dir, "--skip-go-test")
	if runErr != nil {
		t.Fatalf("dockyard test on a clean project failed: %v\n%s", runErr, out)
	}
	if !strings.Contains(out, "test: OK") {
		t.Errorf("dockyard test output should report OK on a clean project:\n%s", out)
	}
	for _, cat := range []string{"contract", "spec-compliance", "capability"} {
		if !strings.Contains(out, cat) {
			t.Errorf("dockyard test output should list the %q category:\n%s", cat, out)
		}
	}
}
