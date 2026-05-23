package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the root command with args, capturing stdout and stderr.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd(&outBuf, &errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestRoot_HelpListsNewCommand(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "new") {
		t.Errorf("root help does not list the 'new' command:\n%s", out)
	}
}

func TestRoot_BarePrintsHelp(t *testing.T) {
	t.Parallel()
	out, _, err := run(t)
	if err != nil {
		t.Fatalf("bare invocation: %v", err)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("bare 'dockyard' did not print help:\n%s", out)
	}
}

func TestNew_ScaffoldsProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _, err := run(t, "new", "cli-demo", "--dir", dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("new output missing 'Created' summary:\n%s", out)
	}
	if !strings.Contains(out, "Next steps") {
		t.Errorf("new output missing next-steps guidance:\n%s", out)
	}
	// The project exists.
	for _, rel := range []string{"dockyard.app.yaml", "main.go", "go.mod"} {
		if _, statErr := os.Stat(filepath.Join(dir, "cli-demo", rel)); statErr != nil {
			t.Errorf("expected scaffolded file %s: %v", rel, statErr)
		}
	}
}

func TestNew_RejectsInvalidName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, _, err := run(t, "new", "Bad_Name", "--dir", dir)
	if err == nil {
		t.Fatal("new with an invalid name: want an error")
	}
	if !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("error does not explain the invalid name: %v", err)
	}
}

func TestNew_RejectsExistingProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, _, err := run(t, "new", "twice", "--dir", dir); err != nil {
		t.Fatalf("first new: %v", err)
	}
	_, _, err := run(t, "new", "twice", "--dir", dir)
	if err == nil {
		t.Fatal("new into an existing project: want an error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error does not explain the existing directory: %v", err)
	}
}

func TestNew_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "new"); err == nil {
		t.Error("new with no name: want an error")
	}
	if _, _, err := run(t, "new", "a", "b"); err == nil {
		t.Error("new with two names: want an error")
	}
}

func TestNew_DockyardPathAddsReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, _, err := run(t, "new", "with-replace", "--dir", dir, "--dockyard-path", "."); err != nil {
		t.Fatalf("new --dockyard-path: %v", err)
	}
	gomod, err := os.ReadFile(filepath.Join(dir, "with-replace", "go.mod")) //nolint:gosec // dir is a test temp dir
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(gomod), "replace github.com/hurtener/dockyard =>") {
		t.Errorf("--dockyard-path did not add a replace directive:\n%s", gomod)
	}
	// The replace path is absolute.
	abs, _ := filepath.Abs(".")
	if !strings.Contains(string(gomod), abs) {
		t.Errorf("replace directive is not absolute (want %s):\n%s", abs, gomod)
	}
}

// TestNew_TemplateFlagListedInHelp proves the --template flag is wired into
// the cobra `new` command's help (Phase 24 acceptance).
func TestNew_TemplateFlagListedInHelp(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "new", "--help")
	if err != nil {
		t.Fatalf("new --help: %v", err)
	}
	if !strings.Contains(out, "--template") {
		t.Errorf("new --help does not list --template:\n%s", out)
	}
}

// TestNew_TemplateUnknown_TypedError proves an unregistered --template
// surfaces ErrUnknownTemplate's CLI mapping rather than a generic error.
func TestNew_TemplateUnknown_TypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, _, err := run(t, "new", "unknown-tpl", "--dir", dir, "--template", "no-such-template")
	if err == nil {
		t.Fatal("new --template no-such-template: want an error")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Errorf("error message does not name the unknown-template case: %v", err)
	}
}
