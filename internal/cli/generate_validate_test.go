package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
)

func TestRoot_HelpListsGenerateAndValidate(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	for _, verb := range []string{"generate", "validate"} {
		if !strings.Contains(out, verb) {
			t.Errorf("root help does not list the %q command:\n%s", verb, out)
		}
	}
}

func TestPrintGenerateResultReportsRemovalsAsChanges(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	printGenerateResult(&out, generate.Result{
		Written: []string{"internal/contracts/contracts.ts"},
		Removed: []string{"internal/contracts/old_output.schema.json"},
	})
	if got := out.String(); !strings.Contains(got, "1 removed") ||
		!strings.Contains(got, "removed  internal/contracts/old_output.schema.json") ||
		strings.Contains(got, "no changes") {
		t.Fatalf("generate output did not report removal:\n%s", got)
	}
}

func TestGenerate_RejectsArgs(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "generate", "extra-arg"); err == nil {
		t.Error("generate with a positional arg: want an error")
	}
}

func TestValidate_RejectsArgs(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "validate", "extra-arg"); err == nil {
		t.Error("validate with a positional arg: want an error")
	}
}

func TestGenerate_FailsWhenNoManifest(t *testing.T) {
	t.Parallel()
	// A directory with no dockyard.app.yaml is not a Dockyard project.
	dir := t.TempDir()
	_, _, err := run(t, "generate", "--dir", dir)
	if err == nil {
		t.Fatal("generate in a non-project directory: want an error")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error should explain the missing manifest: %v", err)
	}
}

func TestValidate_FailsWhenNoManifest(t *testing.T) {
	t.Parallel()
	// validate reports a missing manifest as a build blocker → non-zero exit
	// surfaced as the errBlockers sentinel.
	dir := t.TempDir()
	out, _, err := run(t, "validate", "--dir", dir)
	if err == nil {
		t.Fatal("validate in a non-project directory: want a non-zero (error) exit")
	}
	if !strings.Contains(out, "FAILED") {
		t.Errorf("validate output should report FAILED:\n%s", out)
	}
	if !strings.Contains(out, "manifest") {
		t.Errorf("validate output should name the manifest blocker:\n%s", out)
	}
}

func TestValidate_HelpDescribesGates(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "validate", "--help")
	if err != nil {
		t.Fatalf("validate --help: %v", err)
	}
	for _, want := range []string{"manifest", "schema", "stale"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("validate help does not mention %q:\n%s", want, out)
		}
	}
}
