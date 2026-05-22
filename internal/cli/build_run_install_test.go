package cli

import (
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/buildpkg"
	"github.com/hurtener/dockyard/internal/installpkg"
)

// TestRoot_HelpListsPhase20Verbs proves the cobra root exposes build, run and
// install — the three Phase 20 verbs.
func TestRoot_HelpListsPhase20Verbs(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	for _, verb := range []string{"build", "run", "install"} {
		if !strings.Contains(out, verb) {
			t.Errorf("root help does not list the %q command:\n%s", verb, out)
		}
	}
}

// --- build -------------------------------------------------------------------

func TestBuild_HelpDescribesPipeline(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "build", "--help")
	if err != nil {
		t.Fatalf("build --help: %v", err)
	}
	for _, want := range []string{"validate", "Vite", "CGo-free", "checksum"} {
		if !strings.Contains(out, want) {
			t.Errorf("build --help missing %q:\n%s", want, out)
		}
	}
}

func TestBuild_HasFlags(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "build", "--help")
	if err != nil {
		t.Fatalf("build --help: %v", err)
	}
	for _, flag := range []string{"--dir", "--output", "--cross-compile"} {
		if !strings.Contains(out, flag) {
			t.Errorf("build --help does not document %q:\n%s", flag, out)
		}
	}
}

func TestBuild_RejectsMissingProject(t *testing.T) {
	t.Parallel()
	_, _, err := run(t, "build", "--dir", t.TempDir())
	if err == nil {
		t.Fatal("build accepted a directory with no dockyard.app.yaml")
	}
	if !strings.Contains(err.Error(), "build failed") {
		t.Errorf("build error not wrapped with the verb context: %v", err)
	}
}

func TestBuild_RejectsExtraArgs(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "build", "stray"); err == nil {
		t.Fatal("build accepted a stray positional argument")
	}
}

// --- run ---------------------------------------------------------------------

func TestRun_HelpDescribesTransports(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "run", "--help")
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}
	for _, want := range []string{"stdio", "http", "transport"} {
		if !strings.Contains(out, want) {
			t.Errorf("run --help missing %q:\n%s", want, out)
		}
	}
}

func TestRun_HasFlags(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "run", "--help")
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}
	for _, flag := range []string{"--transport", "--addr", "--dir"} {
		if !strings.Contains(out, flag) {
			t.Errorf("run --help does not document %q:\n%s", flag, out)
		}
	}
}

func TestRun_RejectsUnknownTransport(t *testing.T) {
	t.Parallel()
	_, _, err := run(t, "run", "--transport", "carrier-pigeon", "--dir", t.TempDir())
	if err == nil {
		t.Fatal("run accepted an unknown transport")
	}
	if !strings.Contains(err.Error(), "unknown transport") {
		t.Errorf("run error does not explain the bad transport: %v", err)
	}
}

func TestRun_RejectsExtraArgs(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "run", "stray"); err == nil {
		t.Fatal("run accepted a stray positional argument")
	}
}

// --- install -----------------------------------------------------------------

func TestInstall_HelpDescribesHosts(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "install", "--help")
	if err != nil {
		t.Fatalf("install --help: %v", err)
	}
	for _, want := range []string{"claude", "cursor", "non-destructive", "initialize"} {
		if !strings.Contains(out, want) {
			t.Errorf("install --help missing %q:\n%s", want, out)
		}
	}
}

func TestInstall_HasFlags(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "install", "--help")
	if err != nil {
		t.Fatalf("install --help: %v", err)
	}
	for _, flag := range []string{"--dir", "--binary", "--config"} {
		if !strings.Contains(out, flag) {
			t.Errorf("install --help does not document %q:\n%s", flag, out)
		}
	}
}

func TestInstall_RejectsUnknownHost(t *testing.T) {
	t.Parallel()
	_, _, err := run(t, "install", "notepad")
	if err == nil {
		t.Fatal("install accepted an unknown host")
	}
	if !strings.Contains(err.Error(), "unknown host") {
		t.Errorf("install error does not explain the bad host: %v", err)
	}
}

func TestInstall_RequiresHostArg(t *testing.T) {
	t.Parallel()
	if _, _, err := run(t, "install"); err == nil {
		t.Fatal("install with no host argument: want an error")
	}
	if _, _, err := run(t, "install", "claude", "cursor"); err == nil {
		t.Fatal("install with two host arguments: want an error")
	}
}

// --- helper coverage ---------------------------------------------------------

func TestResolveInstallBinary_Default(t *testing.T) {
	t.Parallel()
	got, err := resolveInstallBinary("/tmp/my-app", "")
	if err != nil {
		t.Fatalf("resolveInstallBinary: %v", err)
	}
	if !strings.Contains(got, "my-app-") || !strings.Contains(got, "dist") {
		t.Errorf("default binary path %q does not name the dist artifact", got)
	}
}

func TestResolveInstallBinary_Explicit(t *testing.T) {
	t.Parallel()
	got, err := resolveInstallBinary("/tmp/proj", "build/server")
	if err != nil {
		t.Fatalf("resolveInstallBinary: %v", err)
	}
	if !strings.HasSuffix(got, "build/server") {
		t.Errorf("explicit binary path not honoured: %q", got)
	}
}

func TestPrintBuildResult(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	printBuildResult(&b, buildpkg.Result{
		UIEmbedded: true,
		Artifacts: []buildpkg.Artifact{{
			Target: buildpkg.Target{OS: "linux", Arch: "amd64"},
			Path:   "/dist/app-linux-amd64", ChecksumPath: "/dist/app-linux-amd64.sha256",
		}},
	})
	out := b.String()
	for _, want := range []string{"1 artifact", "UI embedded: true", "linux/amd64", "checksum"} {
		if !strings.Contains(out, want) {
			t.Errorf("printBuildResult output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintInstallResult(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	printInstallResult(&b, installpkg.Result{
		Host: installpkg.HostClaude, ConfigPath: "/cfg.json",
		BackupPath: "/cfg.json.bak", ServerName: "demo", BootOK: true,
	})
	out := b.String()
	for _, want := range []string{"demo", "claude", "/cfg.json", "backup", "boot"} {
		if !strings.Contains(out, want) {
			t.Errorf("printInstallResult output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintInstallResult_BootFailed(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	printInstallResult(&b, installpkg.Result{
		Host: installpkg.HostCursor, ConfigPath: "/c.json",
		ServerName: "x", BootOK: false,
	})
	if !strings.Contains(b.String(), "FAILED") {
		t.Errorf("a failed boot check should print FAILED:\n%s", b.String())
	}
}
