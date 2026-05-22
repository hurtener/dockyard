package buildpkg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// repoRoot returns the Dockyard repository root — two directories up from this
// test file (internal/buildpkg/<file>). A scaffolded project's go.mod replaces
// the Dockyard import at this path so the build compiles against the real
// runtime library.
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
// project directory — the input a Build operates on.
func scaffoldProject(t *testing.T, name string) string {
	t.Helper()
	parent := t.TempDir()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             parent,
		DockyardReplace: repoRoot(t),
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return res.Dir
}

// TestBuild_HostOnlyProducesArtifactAndChecksum exercises the full Build
// pipeline (regenerate → validate → no-UI skip → go build → checksum) on a
// real scaffolded project, host-only.
func TestBuild_HostOnlyProducesArtifactAndChecksum(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "bp-host")

	res, err := Build(context.Background(), Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("host build produced %d artifacts, want 1", len(res.Artifacts))
	}
	if res.UIEmbedded {
		t.Error("a no-template blank server has no UI — UIEmbedded should be false")
	}
	art := res.Artifacts[0]
	if _, err := os.Stat(art.Path); err != nil {
		t.Errorf("artifact not on disk: %v", err)
	}
	if _, err := os.Stat(art.ChecksumPath); err != nil {
		t.Errorf("checksum not on disk: %v", err)
	}
	if art.Target != hostTarget() {
		t.Errorf("host build target = %v, want %v", art.Target, hostTarget())
	}
}

// TestBuild_CustomOutputDir verifies artifacts land in an explicit OutputDir.
func TestBuild_CustomOutputDir(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "bp-out")
	outDir := t.TempDir()

	res, err := Build(context.Background(), Options{
		ProjectDir: projectDir,
		OutputDir:  outDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := filepath.Dir(res.Artifacts[0].Path); got != outDir {
		t.Errorf("artifact dir = %q, want the explicit output dir %q", got, outDir)
	}
}

// TestBuild_FailsOnValidationBlocker verifies a project with a corrupt
// manifest fails the build at the validate gate (P1 at build time).
func TestBuild_FailsOnValidationBlocker(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "bp-blocked")
	manifestPath := filepath.Join(projectDir, "dockyard.app.yaml")
	if err := os.WriteFile(manifestPath, []byte("name: \nversion: bad\n"), 0o600); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
	_, err := Build(context.Background(), Options{ProjectDir: projectDir})
	if err == nil {
		t.Fatal("Build of a project with a validation blocker: want a failure")
	}
	if !errors.Is(err, ErrValidationBlocked) && !errors.Is(err, ErrBuild) {
		t.Errorf("failure not a typed buildpkg error: %v", err)
	}
}

// TestBuild_SkipValidate verifies the SkipValidate test seam bypasses the
// gate — a build still succeeds even with a contract the gate would warn on.
func TestBuild_SkipValidate(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "bp-skipval")
	res, err := Build(context.Background(), Options{
		ProjectDir:   projectDir,
		SkipValidate: true,
	})
	if err != nil {
		t.Fatalf("Build with SkipValidate: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Errorf("produced %d artifacts, want 1", len(res.Artifacts))
	}
}

// TestBuild_CrossCompileNonHost verifies a non-host target cross-compiles and
// the artifact name encodes the triple.
func TestBuild_CrossCompileNonHost(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "bp-cross")

	var target Target
	for _, tg := range DefaultMatrix() {
		if tg.OS != runtime.GOOS || tg.Arch != runtime.GOARCH {
			target = tg
			break
		}
	}
	res, err := Build(context.Background(), Options{
		ProjectDir: projectDir,
		Targets:    []Target{target},
	})
	if err != nil {
		t.Fatalf("cross-compile to %v: %v", target, err)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Target != target {
		t.Fatalf("cross-compile artifacts = %+v, want one for %v", res.Artifacts, target)
	}
	base := filepath.Base(res.Artifacts[0].Path)
	if target.OS == "windows" && filepath.Ext(base) != ".exe" {
		t.Errorf("windows artifact %q has no .exe suffix", base)
	}
}

// TestBuildViteUI_NoWebProject verifies the Vite step is a clean graceful skip
// when the project has no web/ UI.
func TestBuildViteUI_NoWebProject(t *testing.T) {
	t.Parallel()
	built, err := buildViteUI(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("buildViteUI on a no-web/ project: %v", err)
	}
	if built {
		t.Error("buildViteUI reported a UI built where there is no web/ project")
	}
}
