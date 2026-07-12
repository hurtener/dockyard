package cli

import (
	"strings"
	"testing"
)

func TestRoot_HelpListsDevCommand(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "dev") {
		t.Errorf("root help does not list the 'dev' command:\n%s", out)
	}
}

func TestDev_HelpDescribesLoop(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "dev", "--help")
	if err != nil {
		t.Fatalf("dev --help: %v", err)
	}
	for _, want := range []string{"dev loop", "fsnotify", "Vite", "Ctrl-C", "server/discover", "legacy initialize fallback"} {
		if !strings.Contains(out, want) {
			t.Errorf("dev --help missing %q:\n%s", want, out)
		}
	}
}

func TestDev_HasDirAndDebounceFlags(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "dev", "--help")
	if err != nil {
		t.Fatalf("dev --help: %v", err)
	}
	for _, flag := range []string{"--dir", "--debounce"} {
		if !strings.Contains(out, flag) {
			t.Errorf("dev --help does not document %q:\n%s", flag, out)
		}
	}
}

// TestDev_RejectsMissingProject proves the dev verb surfaces a clean error
// (not a panic) when pointed at a directory that is not a Dockyard project.
func TestDev_RejectsMissingProject(t *testing.T) {
	t.Parallel()
	_, _, err := run(t, "dev", "--dir", t.TempDir())
	if err == nil {
		t.Fatal("dev accepted a directory with no dockyard.app.yaml")
	}
	if !strings.Contains(err.Error(), "dev loop") {
		t.Errorf("dev error not wrapped with the verb context: %v", err)
	}
}

// TestDev_RejectsExtraArgs proves `dockyard dev` takes no positional args.
func TestDev_RejectsExtraArgs(t *testing.T) {
	t.Parallel()
	_, _, err := run(t, "dev", "stray-arg")
	if err == nil {
		t.Fatal("dev accepted a stray positional argument")
	}
}
