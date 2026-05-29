package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the v1.3 --here behaviour (the `dockyard new --here`
// flag): scaffolding into a non-empty directory, the clearer
// names-the-entries rejection without it, and the no-silent-overwrite
// collision guard.

func TestGenerate_NonEmptyDir_RejectedWithEntryNames(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	proj := filepath.Join(parent, "srv")
	if err := os.MkdirAll(proj, 0o750); err != nil {
		t.Fatal(err)
	}
	// A hidden entry — the exact case the downstream user hit.
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := Generate(Options{Name: "srv", Dir: parent})
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("want ErrTargetExists, got %v", err)
	}
	if !strings.Contains(err.Error(), ".git/") {
		t.Errorf("error should name the offending entry; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "--here") {
		t.Errorf("error should mention --here; got %q", err.Error())
	}
}

func TestGenerate_Here_ScaffoldsIntoNonEmptyDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	proj := filepath.Join(parent, "srv")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A pre-existing, non-colliding file must be left untouched.
	keep := filepath.Join(proj, "NOTES.md")
	if err := os.WriteFile(keep, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Generate(Options{Name: "srv", Dir: parent, Here: true})
	if err != nil {
		t.Fatalf("Generate --here into non-empty dir: %v", err)
	}
	if len(res.Files) == 0 {
		t.Fatal("expected files written")
	}
	if got, _ := os.ReadFile(keep); string(got) != "keep me" { //nolint:gosec // test temp dir
		t.Errorf("pre-existing file was clobbered; got %q", got)
	}
	// The scaffold landed.
	if _, err := os.Stat(filepath.Join(proj, "dockyard.app.yaml")); err != nil {
		t.Errorf("scaffold did not write the manifest: %v", err)
	}
}

func TestGenerate_Here_FileCollisionRefused(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	proj := filepath.Join(parent, "srv")
	if err := os.MkdirAll(proj, 0o750); err != nil {
		t.Fatal(err)
	}
	// go.mod is a scaffold output — a pre-existing one must collide, not
	// be silently overwritten.
	existing := filepath.Join(proj, "go.mod")
	if err := os.WriteFile(existing, []byte("module pre.existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Generate(Options{Name: "srv", Dir: parent, Here: true})
	if !errors.Is(err, ErrFileCollision) {
		t.Fatalf("want ErrFileCollision, got %v", err)
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("collision error should name go.mod; got %q", err.Error())
	}
	// The existing file is untouched (collision is checked before any write).
	if got, _ := os.ReadFile(existing); string(got) != "module pre.existing\n" { //nolint:gosec // test temp dir
		t.Errorf("colliding file was modified; got %q", got)
	}
}
