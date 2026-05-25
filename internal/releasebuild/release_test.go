package releasebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/buildpkg"
)

// fixtureProject scaffolds a minimal Go module + main package the release
// driver can `go build` against. It is intentionally tiny — the goal of the
// tests is to exercise the driver's orchestration (matrix iteration,
// artifact naming, checksum writing), not the Go toolchain itself.
//
// The fixture lives under a per-test tempdir so parallel tests do not
// contend. Returns the absolute path of the fixture project root.
func fixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// A go.mod the driver's "is this a Dockyard repo root?" check
	// accepts. We point at the same Go version Dockyard pins
	// elsewhere so a future `go vet` against the fixture is happy.
	mod := `module dockyard-release-test

go 1.26
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil { //nolint:gosec // test fixture in t.TempDir()
		t.Fatalf("write go.mod: %v", err)
	}
	// cmd/dockyard/main.go — the smallest possible Go main package
	// that `go build` will accept. The release driver compiles it
	// once per cross-compile target.
	cmdDir := filepath.Join(dir, "cmd", "dockyard")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil { //nolint:gosec // test fixture in t.TempDir()
		t.Fatalf("mkdir %s: %v", cmdDir, err)
	}
	main := `package main

import "fmt"

func main() { fmt.Println("dockyard-release-test") }
`
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(main), 0o644); err != nil { //nolint:gosec // test fixture in t.TempDir()
		t.Fatalf("write main.go: %v", err)
	}
	return dir
}

// TestRelease_HostOnly drives the release pipeline against the fixture
// project with the matrix narrowed to the host target. Asserts every
// output landed where expected and the checksum lines are well-formed.
//
// Narrowing to the host target keeps the test fast and CGo-free on any
// runner; the matrix coverage itself is verified by
// TestRelease_FullMatrixNamesAreUnique without actually invoking the
// (slow) cross-compiler.
func TestRelease_HostOnly(t *testing.T) {
	t.Parallel()
	projectDir := fixtureProject(t)
	outDir := filepath.Join(t.TempDir(), "release")
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}

	res, err := Release(context.Background(), Options{
		Version:    "v1.0.0",
		ProjectDir: projectDir,
		OutputDir:  outDir,
		Targets:    []Target{host},
		BinaryBase: "dockyard",
	})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if res.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", res.Version, "v1.0.0")
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", len(res.Artifacts))
	}
	a := res.Artifacts[0]
	if a.Target != host {
		t.Errorf("Artifact.Target = %v, want %v", a.Target, host)
	}
	wantName := publishedArtifactName("dockyard", "v1.0.0", host)
	if filepath.Base(a.Path) != wantName {
		t.Errorf("artifact filename = %q, want %q", filepath.Base(a.Path), wantName)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Errorf("artifact %s not on disk: %v", a.Path, err)
	}
	if _, err := os.Stat(a.ChecksumPath); err != nil {
		t.Errorf("checksum %s not on disk: %v", a.ChecksumPath, err)
	}
	if len(a.SHA256) != 64 {
		t.Errorf("SHA256 = %q, want a 64-char hex digest", a.SHA256)
	}

	// The sidecar's body is the standard `sha256sum -c` line shape.
	sidecar, err := os.ReadFile(a.ChecksumPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	wantLine := a.SHA256 + "  " + wantName + "\n"
	if string(sidecar) != wantLine {
		t.Errorf("sidecar = %q, want %q", string(sidecar), wantLine)
	}

	// The aggregate checksums.txt covers the one artifact.
	agg, err := os.ReadFile(res.ChecksumsFile)
	if err != nil {
		t.Fatalf("read aggregate checksums: %v", err)
	}
	if string(agg) != wantLine {
		t.Errorf("aggregate checksums = %q, want %q", string(agg), wantLine)
	}

	// The stage tree is cleaned up — only the publish-named artifacts
	// (and the aggregate) remain in OutputDir.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read outDir: %v", err)
	}
	for _, e := range entries {
		// One artifact + its .sha256 + the aggregate. No stage/.
		if e.Name() == "stage" {
			t.Errorf("stage tree should have been cleaned; saw %v", e)
		}
	}
}

// TestRelease_MissingVersion exercises the Options validation surface.
func TestRelease_MissingVersion(t *testing.T) {
	t.Parallel()
	_, err := Release(context.Background(), Options{OutputDir: t.TempDir()})
	if !errors.Is(err, ErrRelease) {
		t.Errorf("missing Version: expected ErrRelease; got %v", err)
	}
}

// TestRelease_MissingOutputDir exercises the Options validation surface.
func TestRelease_MissingOutputDir(t *testing.T) {
	t.Parallel()
	_, err := Release(context.Background(), Options{Version: "v1.0.0"})
	if !errors.Is(err, ErrRelease) {
		t.Errorf("missing OutputDir: expected ErrRelease; got %v", err)
	}
}

// TestRelease_InvalidVersion rejects shapes that would corrupt the
// artifact filename.
func TestRelease_InvalidVersion(t *testing.T) {
	t.Parallel()
	cases := []string{
		"with space",
		"with/slash",
		"",
	}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			_, err := Release(context.Background(), Options{
				Version:   v,
				OutputDir: t.TempDir(),
			})
			if !errors.Is(err, ErrRelease) {
				t.Errorf("Version %q: expected ErrRelease; got %v", v, err)
			}
		})
	}
}

// TestRelease_NotADockyardRoot fails fast when ProjectDir does not look
// like a Go module root.
func TestRelease_NotADockyardRoot(t *testing.T) {
	t.Parallel()
	_, err := Release(context.Background(), Options{
		Version:    "v1.0.0",
		ProjectDir: t.TempDir(), // no go.mod
		OutputDir:  t.TempDir(),
	})
	if !errors.Is(err, ErrRelease) {
		t.Errorf("expected ErrRelease for a non-module project dir; got %v", err)
	}
}

// TestRelease_AcceptsBareSemver canonicalises "1.0.0" to "v1.0.0".
func TestRelease_AcceptsBareSemver(t *testing.T) {
	t.Parallel()
	projectDir := fixtureProject(t)
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	res, err := Release(context.Background(), Options{
		Version:    "1.0.0",
		ProjectDir: projectDir,
		OutputDir:  filepath.Join(t.TempDir(), "release"),
		Targets:    []Target{host},
		BinaryBase: "dockyard",
	})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if res.Version != "v1.0.0" {
		t.Errorf("canonical Version = %q, want %q", res.Version, "v1.0.0")
	}
	if !strings.Contains(filepath.Base(res.Artifacts[0].Path), "-v1.0.0-") {
		t.Errorf("artifact filename missing v1.0.0 token: %s", res.Artifacts[0].Path)
	}
}

// TestRelease_FullMatrixNamesAreUnique exercises the publish-name shape
// across the whole RFC §14 matrix. It does NOT actually compile —
// publishedArtifactName is pure, so the test stays fast.
func TestRelease_FullMatrixNamesAreUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for _, tgt := range DefaultMatrix() {
		n := publishedArtifactName("dockyard", "v1.0.0", tgt)
		if _, dup := seen[n]; dup {
			t.Errorf("duplicate published name %q", n)
		}
		seen[n] = struct{}{}
		// The Windows targets must carry .exe; non-Windows targets
		// must not.
		switch tgt.OS {
		case "windows":
			if !strings.HasSuffix(n, ".exe") {
				t.Errorf("windows target %v -> %q missing .exe", tgt, n)
			}
		default:
			if strings.HasSuffix(n, ".exe") {
				t.Errorf("non-windows target %v -> %q carries .exe", tgt, n)
			}
		}
	}
	if len(seen) != len(buildpkg.DefaultMatrix()) {
		t.Errorf("publishedArtifactName produced %d unique names; matrix has %d targets",
			len(seen), len(buildpkg.DefaultMatrix()))
	}
}

// TestRelease_TargetReExport is a quick belt-and-braces that the package's
// Target alias resolves to the buildpkg one. The blank-identifier
// assignment exercises the type-alias chain; the explicit type carrier on
// the RHS is the load-bearing assertion.
func TestRelease_TargetReExport(t *testing.T) {
	t.Parallel()
	var _ Target = buildpkg.Target{OS: "linux", Arch: "amd64"} //nolint:staticcheck // QF1011: the explicit alias is the test
}

// TestRelease_OutputDirIsCleanlyShaped confirms the OutputDir contains
// exactly the artifacts + sidecars + the aggregate checksums file — no
// scratch directories left behind.
func TestRelease_OutputDirIsCleanlyShaped(t *testing.T) {
	t.Parallel()
	projectDir := fixtureProject(t)
	outDir := filepath.Join(t.TempDir(), "release")
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	res, err := Release(context.Background(), Options{
		Version:    "v1.0.0",
		ProjectDir: projectDir,
		OutputDir:  outDir,
		Targets:    []Target{host},
	})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read outDir: %v", err)
	}
	// host artifact + its .sha256 sidecar + checksums.txt = 3 entries.
	const wantEntries = 3
	if len(entries) != wantEntries {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("OutputDir has %d entries (%v); want %d", len(entries), names, wantEntries)
	}
	// And every entry should be a regular file.
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("OutputDir entry %q is a directory; release output should be flat", e.Name())
		}
	}
	_ = res
}
