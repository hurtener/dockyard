package devloop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
)

func TestGoServerCommandDefault(t *testing.T) {
	t.Parallel()
	c := goServerCommand("/tmp/proj", nil, nil)
	if c.path != "go" {
		t.Errorf("default go-server path = %q, want %q", c.path, "go")
	}
	if len(c.args) != 2 || c.args[0] != "run" || c.args[1] != "." {
		t.Errorf("default go-server args = %v, want [run .]", c.args)
	}
	if c.dir != "/tmp/proj" {
		t.Errorf("go-server dir = %q, want /tmp/proj", c.dir)
	}
	// CGO_ENABLED=0 must be in the child environment.
	found := false
	for _, e := range c.env {
		if e == "CGO_ENABLED=0" {
			found = true
		}
	}
	if !found {
		t.Error("go-server env missing CGO_ENABLED=0")
	}
}

func TestGenerationHasChangesIncludesRemovals(t *testing.T) {
	t.Parallel()
	if !generationHasChanges(generate.Result{Removed: []string{"old.schema.json"}}) {
		t.Fatal("a removed generated artifact must be reported as a codegen change")
	}
	if generationHasChanges(generate.Result{}) {
		t.Fatal("an empty generation result must not be reported as changed")
	}
}

func TestGoServerCommandOverride(t *testing.T) {
	t.Parallel()
	c := goServerCommand("/tmp/proj", []string{"/bin/echo", "hi"}, nil)
	if c.path != "/bin/echo" || len(c.args) != 1 || c.args[0] != "hi" {
		t.Errorf("override not applied: path=%q args=%v", c.path, c.args)
	}
}

// TestGoServerCommandExtraEnv exercises the v1.1-wave-A extension: the
// dev loop appends inspector auto-attach env pins (DOCKYARD_TRANSPORT,
// DOCKYARD_HTTP_ADDR) to the child environment when the inspector is
// enabled. The orchestrator passes these only for variables not already set by
// the developer.
func TestGoServerCommandExtraEnv(t *testing.T) {
	t.Parallel()
	extras := []string{"DOCKYARD_TRANSPORT=http", "DOCKYARD_HTTP_ADDR=127.0.0.1:9999"}
	c := goServerCommand("/tmp/proj", nil, extras)
	var sawTransport, sawAddr bool
	for _, e := range c.env {
		if e == "DOCKYARD_TRANSPORT=http" {
			sawTransport = true
		}
		if e == "DOCKYARD_HTTP_ADDR=127.0.0.1:9999" {
			sawAddr = true
		}
	}
	if !sawTransport {
		t.Error("go-server env missing DOCKYARD_TRANSPORT=http extra")
	}
	if !sawAddr {
		t.Error("go-server env missing DOCKYARD_HTTP_ADDR extra")
	}
}

func TestGoServerExtraEnvPreservesExplicitSettings(t *testing.T) {
	t.Setenv("DOCKYARD_TRANSPORT", "stdio")
	t.Setenv("DOCKYARD_HTTP_ADDR", "127.0.0.1:9444")
	o := &orchestrator{}
	if got := o.goServerExtraEnv("127.0.0.1:8080"); len(got) != 0 {
		t.Fatalf("goServerExtraEnv = %v, want no overrides for explicit settings", got)
	}
	if got := o.serverHTTPAddr(); got != "127.0.0.1:9444" {
		t.Fatalf("serverHTTPAddr = %q, want explicit environment address", got)
	}
	if got := inspectorServerURL(o.serverHTTPAddr()); got != "" {
		t.Fatalf("inspectorServerURL = %q, want detached inspector for explicit stdio", got)
	}
}

func TestViteCommandDefault(t *testing.T) {
	t.Parallel()
	c := viteCommand("/tmp/proj/web", nil)
	if c.path != "npm" {
		t.Errorf("default vite path = %q, want npm", c.path)
	}
	if len(c.args) != 2 || c.args[0] != "run" || c.args[1] != "dev" {
		t.Errorf("default vite args = %v, want [run dev]", c.args)
	}
}

func TestViteCommandOverride(t *testing.T) {
	t.Parallel()
	c := viteCommand("/tmp/proj/web", []string{"/bin/true"})
	if c.path != "/bin/true" || len(c.args) != 0 {
		t.Errorf("override not applied: path=%q args=%v", c.path, c.args)
	}
}

func TestDetectViteProject(t *testing.T) {
	t.Parallel()
	// No web/ directory — not detected.
	dir := t.TempDir()
	if _, found := detectViteProject(dir); found {
		t.Error("detectViteProject reported a Vite project where there is none")
	}

	// A web/ directory with no package.json — still not detected.
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o750); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	if _, found := detectViteProject(dir); found {
		t.Error("detectViteProject reported a Vite project for a web/ with no package.json")
	}

	// A web/package.json — detected.
	if err := os.WriteFile(filepath.Join(dir, "web", "package.json"),
		[]byte(`{"name":"ui"}`), 0o600); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	got, found := detectViteProject(dir)
	if !found {
		t.Fatal("detectViteProject did not detect a web/package.json project")
	}
	if got != filepath.Join(dir, "web") {
		t.Errorf("detected web dir = %q, want %q", got, filepath.Join(dir, "web"))
	}
}

func TestIsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if !isDir(dir) {
		t.Error("isDir reported false for a real directory")
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if isDir(file) {
		t.Error("isDir reported true for a regular file")
	}
	if isDir(filepath.Join(dir, "missing")) {
		t.Error("isDir reported true for a nonexistent path")
	}
}
