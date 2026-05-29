package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the v1.3 go.mod version pin (item 2): a real release
// version is pinned in the require directive so a project that drops the
// replace resolves the published module; the dev placeholder stays v0.0.0.

func TestRequireVersion(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"v1.3.0":      "v1.3.0",
		"1.3.0":       "v1.3.0", // gets the v prefix
		"v1.2.0-rc.1": "v1.2.0-rc.1",
		"0.0.0-dev":   "v0.0.0", // the dev placeholder
		"(devel)":     "v0.0.0",
		"":            "v0.0.0",
		"garbage":     "v0.0.0",
		"v1":          "v0.0.0", // not full semver
	}
	for in, want := range cases {
		if got := requireVersion(in); got != want {
			t.Errorf("requireVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerate_PinsReleaseVersion(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	// A released CLI scaffolding WITHOUT --dockyard-path: the require must
	// pin the real version (no replace), so `go mod tidy` resolves the
	// published module instead of failing on v0.0.0.
	if _, err := Generate(Options{Name: "pinned", Dir: parent, DockyardVersion: "v1.3.0"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(parent, "pinned", "go.mod")) //nolint:gosec // test reads a file under a test temp dir
	if err != nil {
		t.Fatal(err)
	}
	goMod := string(b)
	if !strings.Contains(goMod, "require "+dockyardModule+" v1.3.0") {
		t.Errorf("go.mod did not pin v1.3.0:\n%s", goMod)
	}
	if strings.Contains(goMod, "v0.0.0") {
		t.Errorf("go.mod still carries the v0.0.0 placeholder:\n%s", goMod)
	}
	if strings.Contains(goMod, "replace ") {
		t.Errorf("go.mod has a replace directive without --dockyard-path:\n%s", goMod)
	}
}
