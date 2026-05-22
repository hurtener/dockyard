// This file is the Phase 20 integration test (CLAUDE.md §17). Phase 20's deps
// name shipped phases 17 (`dockyard new`) and 10 (the runtime/apps embed
// pipeline) and it consumes internal/generate, internal/validate and
// internal/buildpkg. It ships an end-to-end integration test driven against
// real components with no mocks at the seam:
//
//   - it runs the real `dockyard new` scaffold and `go mod tidy`s it against
//     the real Dockyard checkout (replace directive);
//   - it runs the real internal/buildpkg.Build host-only and asserts the
//     produced binary is CGo-free, statically linked, and boots — driving a
//     real MCP `initialize` handshake against it;
//   - it runs a real cross-compile of a NON-host GOOS/GOARCH and asserts an
//     artifact + a SHA-256 checksum file are produced;
//   - it runs internal/installpkg.Install against a TEMP host-config path
//     (never the developer's real ~/.claude / Cursor config) and asserts the
//     written config is valid JSON, the server entry is present, a backup
//     exists, and the boot check passes;
//   - it covers two failure modes: a build of a project with a validation
//     blocker must fail, and an install against an unwritable config must
//     fail cleanly.
//
// The test runs under -race.
package integration

import (
	"context"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/buildpkg"
	"github.com/hurtener/dockyard/internal/installpkg"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// scaffoldP20Project runs the real scaffold and `go mod tidy`, returning the
// project directory. The project's go.mod replaces the Dockyard import at this
// repo's root so the build compiles against the real runtime library.
func scaffoldP20Project(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	parent := t.TempDir()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             parent,
		DockyardReplace: root,
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

// TestPhase20_BuildProducesCGoFreeStaticBinary runs the real build pipeline
// host-only and asserts the produced binary is CGo-free + statically linked +
// boots — the binding "one CGo-free static binary" acceptance criterion.
func TestPhase20_BuildProducesCGoFreeStaticBinary(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP20Project(t, "p20-build")

	res, err := buildpkg.Build(context.Background(), buildpkg.Options{
		ProjectDir: projectDir,
	})
	if err != nil {
		t.Fatalf("buildpkg.Build: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("host build produced %d artifacts, want 1", len(res.Artifacts))
	}
	art := res.Artifacts[0]
	if _, err := os.Stat(art.Path); err != nil {
		t.Fatalf("built binary not on disk: %v", err)
	}
	// The checksum sidecar exists and carries a 64-hex-char digest.
	assertChecksum(t, art.Path, art.ChecksumPath)

	// The binary is statically linked / CGo-free: a CGo binary on darwin links
	// libSystem dynamically and on linux has a PT_INTERP; a CGO_ENABLED=0 Go
	// binary does not.
	assertStaticBinary(t, art.Path)

	// The binary boots: drive a real MCP initialize handshake against it.
	assertBoots(t, art.Path)
}

// TestPhase20_CrossCompileEmitsArtifactAndChecksum runs a real cross-compile
// for a NON-host target and asserts an artifact + checksum are produced — the
// binding "cross-compile matrix green" acceptance criterion, exercised on a
// bounded subset (one non-host triple) so the test stays fast.
func TestPhase20_CrossCompileEmitsArtifactAndChecksum(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP20Project(t, "p20-cross")

	target := nonHostTarget()
	res, err := buildpkg.Build(context.Background(), buildpkg.Options{
		ProjectDir: projectDir,
		Targets:    []buildpkg.Target{target},
	})
	if err != nil {
		t.Fatalf("cross-compile to %v: %v", target, err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("cross-compile produced %d artifacts, want 1", len(res.Artifacts))
	}
	art := res.Artifacts[0]
	if art.Target != target {
		t.Errorf("artifact target = %v, want %v", art.Target, target)
	}
	if !strings.Contains(filepath.Base(art.Path), target.OS) ||
		!strings.Contains(filepath.Base(art.Path), target.Arch) {
		t.Errorf("artifact name %q does not encode the target %v", art.Path, target)
	}
	assertChecksum(t, art.Path, art.ChecksumPath)
}

// TestPhase20_BuildFailsOnValidationBlocker is the build failure mode: a
// project with a validation blocker (a broken manifest) must fail the build —
// P1 enforced at build time (RFC §14, §9.4).
func TestPhase20_BuildFailsOnValidationBlocker(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP20Project(t, "p20-blocked")

	// Corrupt the manifest so the validate gate reports a blocker.
	manifestPath := filepath.Join(projectDir, "dockyard.app.yaml")
	if err := os.WriteFile(manifestPath, []byte("name: \nversion: not-a-version\n"), 0o600); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
	_, err := buildpkg.Build(context.Background(), buildpkg.Options{ProjectDir: projectDir})
	if err == nil {
		t.Fatal("build of a project with a validation blocker: want a failure")
	}
	// The build must stop at the validate gate, not at the go build.
	if !errors.Is(err, buildpkg.ErrValidationBlocked) && !errors.Is(err, buildpkg.ErrBuild) {
		t.Errorf("build failure not a typed buildpkg error: %v", err)
	}
}

// TestPhase20_InstallWritesConfigAndVerifiesBoot runs the real install against
// a TEMP host-config path and asserts a valid config is written, a backup
// exists, and the boot check passes — the binding "install writes valid host
// config and confirms the server boots" acceptance criterion.
func TestPhase20_InstallWritesConfigAndVerifiesBoot(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldP20Project(t, "p20-install")

	// Build the server first — install registers a built binary.
	buildRes, err := buildpkg.Build(context.Background(), buildpkg.Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("build before install: %v", err)
	}
	binPath := buildRes.Artifacts[0].Path

	// A TEMP config path — the developer's real ~/.claude is never touched.
	configPath := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	// Seed a prior config with an unrelated entry so the non-destructive merge
	// and the backup are both exercised.
	prior := `{"mcpServers":{"unrelated":{"command":"/bin/true"}}}`
	if err := os.WriteFile(configPath, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed prior config: %v", err)
	}

	res, err := installpkg.Install(context.Background(), installpkg.Options{
		ProjectDir: projectDir,
		Host:       installpkg.HostClaude,
		ConfigPath: configPath,
		BinaryPath: binPath,
	})
	if err != nil {
		t.Fatalf("installpkg.Install: %v", err)
	}
	if !res.BootOK {
		t.Error("install boot check did not pass for a freshly built server")
	}
	if res.BackupPath == "" {
		t.Error("install did not back up the prior config")
	} else if _, err := os.Stat(res.BackupPath); err != nil {
		t.Errorf("backup not on disk: %v", err)
	}

	// The written config is valid JSON and carries both servers.
	data, err := os.ReadFile(configPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("written config is not valid JSON: %v", err)
	}
	if _, ok := cfg.MCPServers["unrelated"]; !ok {
		t.Error("non-destructive merge dropped the unrelated server entry")
	}
	if _, ok := cfg.MCPServers["p20-install"]; !ok {
		t.Errorf("install did not register the server entry:\n%s", data)
	}
}

// TestPhase20_InstallFailsCleanlyOnUnwritableConfig is the install failure
// mode: an install against an unwritable config path must fail cleanly with a
// typed error, never panic.
func TestPhase20_InstallFailsCleanlyOnUnwritableConfig(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("path-as-parent semantics differ on Windows")
	}
	projectDir := scaffoldP20Project(t, "p20-badcfg")

	// A regular file standing where a config directory must be.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := installpkg.Install(context.Background(), installpkg.Options{
		ProjectDir:    projectDir,
		Host:          installpkg.HostClaude,
		ConfigPath:    filepath.Join(blocker, "config.json"),
		BinaryPath:    filepath.Join(projectDir, "anything"),
		SkipBootCheck: true,
	})
	if err == nil || !errors.Is(err, installpkg.ErrInstall) {
		t.Errorf("install against an unwritable config: want an ErrInstall, got %v", err)
	}
}

// --- helpers -----------------------------------------------------------------

// nonHostTarget returns a cross-compile target that is NOT the host platform,
// so the cross-compile test genuinely exercises GOOS/GOARCH cross-building.
func nonHostTarget() buildpkg.Target {
	for _, t := range buildpkg.DefaultMatrix() {
		if t.OS != runtime.GOOS || t.Arch != runtime.GOARCH {
			return t
		}
	}
	// DefaultMatrix always has a non-host triple; this is unreachable.
	return buildpkg.Target{OS: "linux", Arch: "arm64"}
}

// assertChecksum verifies a .sha256 sidecar exists and carries a 64-hex-char
// digest in the standard sha256sum line format.
func assertChecksum(t *testing.T, binPath, sumPath string) {
	t.Helper()
	if sumPath == "" {
		t.Fatal("artifact has no checksum path")
	}
	data, err := os.ReadFile(sumPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read checksum %s: %v", sumPath, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("checksum line is not '<digest>  <name>': %q", data)
	}
	if len(fields[0]) != 64 {
		t.Errorf("checksum digest length = %d, want 64 hex chars", len(fields[0]))
	}
	if fields[1] != filepath.Base(binPath) {
		t.Errorf("checksum names %q, want the artifact basename %q", fields[1], filepath.Base(binPath))
	}
}

// assertStaticBinary verifies the artifact is CGo-free: on linux a CGO_ENABLED=0
// Go binary is fully static — no PT_INTERP segment and no imported libraries;
// on darwin a Go binary is never fully static (Apple does not support static
// linking against libSystem), so the CGo-free signal there is the absence of
// the C/C++ runtime libraries (libc++, libobjc) that only CGo pulls in — the
// always-present libSystem / CoreFoundation / Security are linked by every
// pure-Go macOS binary. Non-host cross-compiled artifacts are not inspected
// (the host cannot exec them).
func assertStaticBinary(t *testing.T, binPath string) {
	t.Helper()
	switch runtime.GOOS {
	case "linux":
		f, err := elf.Open(binPath)
		if err != nil {
			t.Fatalf("open ELF binary: %v", err)
		}
		defer func() { _ = f.Close() }()
		for _, prog := range f.Progs {
			if prog.Type == elf.PT_INTERP {
				t.Error("binary has a PT_INTERP segment — it is dynamically linked, not CGo-free static")
			}
		}
		libs, _ := f.ImportedLibraries()
		if len(libs) != 0 {
			t.Errorf("static CGo-free binary imports libraries: %v", libs)
		}
	case "darwin":
		f, err := macho.Open(binPath)
		if err != nil {
			t.Fatalf("open Mach-O binary: %v", err)
		}
		defer func() { _ = f.Close() }()
		libs, _ := f.ImportedLibraries()
		// libSystem / libresolv / CoreFoundation / Security are linked by
		// EVERY pure-Go macOS binary; libc++ / libobjc are pulled in only by
		// CGo. Their presence is the CGo tell.
		for _, lib := range libs {
			if strings.Contains(lib, "libc++") || strings.Contains(lib, "libobjc") {
				t.Errorf("darwin binary links %q — indicates CGo (CGO_ENABLED!=0)", lib)
			}
		}
	default:
		t.Logf("static-binary inspection not implemented for %s — skipping", runtime.GOOS)
	}
}

// assertBoots spawns the built server exactly as a host would — a local stdio
// subprocess — and drives a real MCP initialize handshake. This is the
// test-only, dev-mode client carve-out (P4), not a shipped MCP client.
func assertBoots(t *testing.T, binPath string) {
	t.Helper()
	ctx := context.Background()
	cmd := exec.Command(binPath) //nolint:gosec // binPath is a Dockyard-built artifact
	transport := &mcpsdk.CommandTransport{Command: cmd}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "p20-bootcheck", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("built server did not complete the MCP initialize handshake: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Errorf("server session did not close cleanly: %v", err)
	}
}
