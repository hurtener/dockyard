package scaffold

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden regenerates the scaffold golden fixtures. Run:
//
//	go test ./internal/scaffold/ -run TestGolden -update
//
// then review the diff. The golden test pins the scaffolded project (fixed
// Options → fixed file tree): an accidental change to any scaffolded file
// fails CI as a visible diff rather than slipping through (CLAUDE.md §11).
var updateGolden = flag.Bool("update", false, "regenerate scaffold golden files")

// goldenName is the project name the golden fixtures are generated under. It
// is fixed so the golden tree is deterministic. goldenModule / goldenReplace
// are also fixed so go.mod is reproducible.
const (
	goldenName    = "golden-server"
	goldenModule  = "example.com/golden-server"
	goldenReplace = "" // no replace directive in the golden fixture
)

// TestGolden pins every file of the no-template scaffold against testdata/golden.
func TestGolden(t *testing.T) {
	dir := t.TempDir()
	res, err := Generate(Options{
		Name:            goldenName,
		Dir:             dir,
		ModulePath:      goldenModule,
		DockyardReplace: goldenReplace,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	goldenDir := filepath.Join("testdata", "golden")
	for _, rel := range res.Files {
		got, err := os.ReadFile(filepath.Join(res.Dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read generated %s: %v", rel, err)
		}
		// The golden file mirrors the project-relative path, with a .golden
		// suffix so it is never mistaken for buildable source.
		goldenPath := filepath.Join(goldenDir, filepath.FromSlash(rel)+".golden")

		if *updateGolden {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil {
				t.Fatalf("mkdir golden dir: %v", err)
			}
			//nolint:gosec // goldenPath is a composed testdata path under -update only
			if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
				t.Fatalf("write golden %s: %v", goldenPath, err)
			}
			continue
		}

		want, err := os.ReadFile(goldenPath) //nolint:gosec // goldenPath is a composed testdata path
		if err != nil {
			t.Fatalf("read golden %s (run with -update to create): %v", goldenPath, err)
		}
		if string(got) != string(want) {
			t.Errorf("scaffolded %s differs from golden — run -update and review:\n--- got ---\n%s\n--- want ---\n%s",
				rel, got, want)
		}
	}
}
