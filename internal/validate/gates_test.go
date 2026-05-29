package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
)

// These tests cover the v1.3 enforcement of the previously-dead quality gates
// (D-168, D-169): require_fixtures (UI-scoped) and require_contract_tests
// (project-wide). The checks are exercised directly so the assertions are
// isolated from the rest of the validate pipeline.

func blockers(rp *reporter, c Check) int {
	n := 0
	for _, d := range rp.diagnostics {
		if d.Check == c && d.Severity == Blocker {
			n++
		}
	}
	return n
}

func TestCheckFixtures_UIScoped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	uiTool := manifest.Tool{Name: "widget", UI: "app"}
	nonUITool := manifest.Tool{Name: "greet"}

	// Gate off → never fires.
	rp := &reporter{}
	checkFixtures(rp, dir, loadedManifest{m: &manifest.Manifest{
		Quality: manifest.Quality{RequireFixtures: false},
		Tools:   []manifest.Tool{uiTool},
	}})
	if got := blockers(rp, CheckFixtures); got != 0 {
		t.Errorf("gate off: want 0 blockers, got %d", got)
	}

	// Gate on, UI tool, no fixtures → Blocker; non-UI tool ignored.
	rp = &reporter{}
	checkFixtures(rp, dir, loadedManifest{m: &manifest.Manifest{
		Quality: manifest.Quality{RequireFixtures: true},
		Tools:   []manifest.Tool{uiTool, nonUITool},
	}})
	if got := blockers(rp, CheckFixtures); got != 1 {
		t.Fatalf("UI tool missing fixtures: want 1 blocker, got %d", got)
	}

	// Add a fixture for the UI tool → green.
	fxDir := filepath.Join(dir, "fixtures", "widget")
	if err := os.MkdirAll(fxDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fxDir, "happy.json"), []byte(`{"state":"happy"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rp = &reporter{}
	checkFixtures(rp, dir, loadedManifest{m: &manifest.Manifest{
		Quality: manifest.Quality{RequireFixtures: true},
		Tools:   []manifest.Tool{uiTool, nonUITool},
	}})
	if got := blockers(rp, CheckFixtures); got != 0 {
		t.Errorf("UI tool with a fixture: want 0 blockers, got %d", got)
	}
}

func TestCheckContractTests_ProjectWide(t *testing.T) {
	t.Parallel()
	lm := loadedManifest{m: &manifest.Manifest{
		Quality: manifest.Quality{RequireContractTests: true},
		Tools:   []manifest.Tool{{Name: "greet"}},
	}}

	// No test file → Blocker (the downstream "deleted greet_test.go" case).
	empty := t.TempDir()
	rp := &reporter{}
	checkContractTests(rp, empty, lm)
	if got := blockers(rp, CheckContractTests); got != 1 {
		t.Fatalf("no test: want 1 blocker, got %d", got)
	}

	// A *_test.go anywhere (outside web/, vendor, node_modules) → green.
	withTest := t.TempDir()
	if err := os.WriteFile(filepath.Join(withTest, "greet_test.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rp = &reporter{}
	checkContractTests(rp, withTest, lm)
	if got := blockers(rp, CheckContractTests); got != 0 {
		t.Errorf("with a test: want 0 blockers, got %d", got)
	}

	// A test only under web/ does not count (frontend tree is skipped).
	webOnly := t.TempDir()
	webDir := filepath.Join(webOnly, "web")
	if err := os.MkdirAll(webDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "x_test.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rp = &reporter{}
	checkContractTests(rp, webOnly, lm)
	if got := blockers(rp, CheckContractTests); got != 1 {
		t.Errorf("test only under web/: want 1 blocker, got %d", got)
	}

	// Gate off → never fires.
	rp = &reporter{}
	checkContractTests(rp, empty, loadedManifest{m: &manifest.Manifest{
		Quality: manifest.Quality{RequireContractTests: false},
	}})
	if got := blockers(rp, CheckContractTests); got != 0 {
		t.Errorf("gate off: want 0 blockers, got %d", got)
	}
}
