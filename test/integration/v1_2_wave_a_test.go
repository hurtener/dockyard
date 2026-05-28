// This file is the v1.2 wave A integration test (CLAUDE.md §17). Wave A
// closes the D-139 manual post-scaffold workflow: `dockyard new` now runs
// `go mod tidy` + `dockyard generate` for the developer at scaffold time
// (D-166), so a fresh project — including a --template scaffold, which ships
// Go contracts but not the generated JSON Schema + TypeScript — reaches a
// green `dockyard validate` on the first try with no manual command.
//
// The end-to-end proof uses real drivers at the seam (no mocks): a real
// scaffolded analytics-widgets project, the real Go toolchain for
// `go mod tidy`, the real codegen pipeline (internal/generate.Run — the
// exact call the CLI post-step makes), and the real validate gate
// (internal/validate.Run). The positive case asserts validate is green only
// after generate ran; the negative case asserts that WITHOUT generate (the
// --no-postgen state) validate flags stale/missing codegen — proving the
// post-step is what makes the difference. It runs under -race.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/internal/validate"
)

// scaffoldWidgetsAndTidy materialises the analytics-widgets template into a
// fresh temp dir with a replace directive at this repo, then runs the real
// `go mod tidy` — the first half of the post-scaffold step (D-166). It returns
// the project dir and the loaded manifest.
func scaffoldWidgetsAndTidy(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := repoRoot(t)
	parent := t.TempDir()

	res, err := scaffold.GenerateFromTemplate(scaffold.Options{
		Name:            "wave-a-widgets",
		Dir:             parent,
		DockyardReplace: root,
		DockyardWebPath: filepath.Join(root, "web"),
	}, "analytics-widgets")
	if err != nil {
		t.Fatalf("scaffold.GenerateFromTemplate(analytics-widgets): %v", err)
	}

	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	m, err := manifest.LoadFile(filepath.Join(res.Dir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load scaffolded manifest: %v", err)
	}
	return res.Dir, m
}

// TestV1_2_WaveA_PostgenMakesValidateGreen is the headline D-166 proof: a
// template scaffold that has had `go mod tidy` + the codegen pipeline run
// (exactly what the CLI post-step does) validates clean on the first call.
func TestV1_2_WaveA_PostgenMakesValidateGreen(t *testing.T) {
	t.Parallel()
	proj, m := scaffoldWidgetsAndTidy(t)

	// The second half of the post-scaffold step: the same codegen call the
	// CLI's generateFn makes (internal/generate.Run).
	if _, err := generate.Run(generate.Options{ProjectDir: proj, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	rp, err := validate.Run(validate.Options{ProjectDir: proj})
	if err != nil {
		t.Fatalf("validate.Run: %v", err)
	}
	if rp.HasBlockers() {
		t.Fatalf("validate reported blockers after the post-scaffold step — D-166 broken:\n%v",
			rp.Blockers())
	}
}

// TestV1_2_WaveA_NoPostgenLeavesValidateRed is the negative control: WITHOUT
// the generate half (the --no-postgen state), a template scaffold's generated
// JSON Schema + TypeScript are missing/stale, so validate reports at least one
// blocker. This proves the post-step is load-bearing — the opt-out genuinely
// changes the outcome, so the default-on behaviour is what earns the
// "green on the first try" promise.
func TestV1_2_WaveA_NoPostgenLeavesValidateRed(t *testing.T) {
	t.Parallel()
	proj, _ := scaffoldWidgetsAndTidy(t)

	// Deliberately skip generate.Run — the --no-postgen path.
	rp, err := validate.Run(validate.Options{ProjectDir: proj})
	if err != nil {
		t.Fatalf("validate.Run: %v", err)
	}
	if !rp.HasBlockers() {
		t.Fatalf("expected validate to flag missing/stale codegen without the post-step, got a clean report")
	}
}
