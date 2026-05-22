package buildpkg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDefaultMatrix verifies the cross-compile matrix is exactly the RFC §14
// set — darwin/linux/windows x amd64/arm64 — and is stably ordered.
func TestDefaultMatrix(t *testing.T) {
	t.Parallel()
	got := DefaultMatrix()
	want := []Target{
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"linux", "amd64"}, {"linux", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	}
	if len(got) != len(want) {
		t.Fatalf("DefaultMatrix len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("matrix[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestTargetBinarySuffix verifies the Windows triple gets a .exe suffix and no
// other does.
func TestTargetBinarySuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target Target
		want   string
	}{
		{Target{"windows", "amd64"}, ".exe"},
		{Target{"windows", "arm64"}, ".exe"},
		{Target{"linux", "amd64"}, ""},
		{Target{"darwin", "arm64"}, ""},
	}
	for _, tt := range tests {
		if got := tt.target.binarySuffix(); got != tt.want {
			t.Errorf("%v.binarySuffix() = %q, want %q", tt.target, got, tt.want)
		}
	}
}

// TestTargetValidate verifies an empty OS or Arch is rejected.
func TestTargetValidate(t *testing.T) {
	t.Parallel()
	if err := (Target{OS: "linux", Arch: "amd64"}).validate(); err != nil {
		t.Errorf("valid target rejected: %v", err)
	}
	for _, bad := range []Target{{OS: "", Arch: "amd64"}, {OS: "linux", Arch: ""}} {
		if err := bad.validate(); err == nil {
			t.Errorf("invalid target %v accepted", bad)
		} else if !errors.Is(err, ErrBuild) {
			t.Errorf("target validate error not wrapping ErrBuild: %v", err)
		}
	}
}

// TestWriteChecksum verifies a checksum sidecar is written in the standard
// sha256sum line format and the digest matches the file.
func TestWriteChecksum(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "artifact")
	if err := os.WriteFile(bin, []byte("hello dockyard"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	sumPath, err := writeChecksum(bin)
	if err != nil {
		t.Fatalf("writeChecksum: %v", err)
	}
	if sumPath != bin+checksumExt {
		t.Errorf("checksum path = %q, want %q", sumPath, bin+checksumExt)
	}
	data, err := os.ReadFile(sumPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	line := string(data)
	if !strings.HasSuffix(strings.TrimSpace(line), "  artifact") {
		t.Errorf("checksum line does not end with the basename: %q", line)
	}
	digest, err := sha256File(bin)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	if !strings.HasPrefix(line, digest) {
		t.Errorf("checksum line %q does not start with the digest %q", line, digest)
	}
	if len(digest) != 64 {
		t.Errorf("sha256 digest length = %d, want 64 hex chars", len(digest))
	}
}

// TestDetectViteProject verifies a web/package.json is the Vite-project signal.
func TestDetectViteProject(t *testing.T) {
	t.Parallel()
	t.Run("no web dir", func(t *testing.T) {
		t.Parallel()
		if _, found := detectViteProject(t.TempDir()); found {
			t.Error("detected a Vite project where there is none")
		}
	})
	t.Run("web with package.json", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		web := filepath.Join(dir, "web")
		if err := os.MkdirAll(web, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(web, "package.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, found := detectViteProject(dir)
		if !found {
			t.Fatal("did not detect the Vite project")
		}
		if got != web {
			t.Errorf("Vite dir = %q, want %q", got, web)
		}
	})
}

// TestBuild_RejectsMissingProject verifies Build fails cleanly when the dir is
// not a Dockyard project (no dockyard.app.yaml) — wrapping ErrBuild.
func TestBuild_RejectsMissingProject(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), Options{ProjectDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build against a non-project: want an error")
	}
	if !errors.Is(err, ErrBuild) {
		t.Errorf("error does not wrap ErrBuild: %v", err)
	}
}

// TestBuild_RequiresProjectDir verifies an empty ProjectDir is a clear error.
func TestBuild_RequiresProjectDir(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), Options{})
	if err == nil || !errors.Is(err, ErrBuild) {
		t.Errorf("empty ProjectDir: want an ErrBuild, got %v", err)
	}
}

// TestHostTarget verifies hostTarget reports the running platform.
func TestHostTarget(t *testing.T) {
	t.Parallel()
	got := hostTarget()
	if got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Errorf("hostTarget() = %v, want %s/%s", got, runtime.GOOS, runtime.GOARCH)
	}
}
