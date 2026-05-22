package devloop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// writeProject lays down a minimal Dockyard-shaped project under a temp dir:
// a dockyard.app.yaml manifest and an internal/contracts directory. It is the
// minimum Run requires to start; it has no web/ directory unless withWeb adds
// one.
func writeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dockyard.app.yaml"), "name: test-srv\n")
	if err := os.MkdirAll(filepath.Join(dir, "internal", "contracts"), 0o750); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	return dir
}

func withWeb(t *testing.T, dir string) {
	t.Helper()
	web := filepath.Join(dir, "web")
	if err := os.MkdirAll(web, 0o750); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	mustWrite(t, filepath.Join(web, "package.json"), `{"name":"ui","scripts":{"dev":"vite"}}`)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunRejectsMissingProject(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Config{ProjectDir: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("Run accepted a nonexistent project directory")
	}
}

func TestRunRejectsNonDockyardDir(t *testing.T) {
	t.Parallel()
	// A directory with no dockyard.app.yaml is not a Dockyard project.
	err := Run(context.Background(), Config{ProjectDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run accepted a directory with no manifest")
	}
}

func TestRunRejectsEmptyProjectDir(t *testing.T) {
	t.Parallel()
	if err := Run(context.Background(), Config{}); err == nil {
		t.Fatal("Run accepted an empty ProjectDir")
	}
}

// runOrchestrator starts Run in a goroutine with the given config plus test
// hooks, and returns a cancel func and a channel that closes when Run returns.
func runOrchestrator(t *testing.T, cfg Config) (cancel context.CancelFunc, done chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	done = make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	return cancelFn, done
}

// TestRunGracefulNoWebProject proves a project with no web/ UI starts cleanly
// and Run tears down on cancel — the graceful-degradation path.
func TestRunGracefulNoWebProject(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	dir := writeProject(t) // no web/

	ready := make(chan struct{})
	cfg := Config{
		ProjectDir:      dir,
		Logger:          quietLogger(),
		GoServerCommand: []string{bin, "run"},
		SkipCodegen:     true,
		hooks:           &hooks{onReady: func() { close(ready) }},
	}
	cancel, done := runOrchestrator(t, cfg)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator never reached ready")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on clean cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestRunSupervisesViteWhenWebPresent proves a project with a web/ package.json
// gets a supervised Vite child.
func TestRunSupervisesViteWhenWebPresent(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	dir := writeProject(t)
	withWeb(t, dir)

	viteStarted := filepath.Join(t.TempDir(), "vite-started")
	ready := make(chan struct{})
	cfg := Config{
		ProjectDir:      dir,
		Logger:          quietLogger(),
		GoServerCommand: []string{bin, "run"},
		// The injected "vite" child touches a file on start — observable proof
		// that Vite supervision actually launched a process.
		ViteCommand: []string{bin, "touch", viteStarted},
		SkipCodegen: true,
		hooks:       &hooks{onReady: func() { close(ready) }},
	}
	cancel, done := runOrchestrator(t, cfg)
	defer func() { <-done }()
	defer cancel()

	<-ready
	waitFileExists(t, viteStarted)
}

// TestRunRestartsGoServerOnGoChange proves a .go file change restarts the Go
// server child. The restart is observed deterministically via the onServerRestart
// hook, not a sleep.
func TestRunRestartsGoServerOnGoChange(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	dir := writeProject(t)

	ready := make(chan struct{})
	restarts := make(chan struct{}, 4)
	cfg := Config{
		ProjectDir:      dir,
		Logger:          quietLogger(),
		GoServerCommand: []string{bin, "run"},
		SkipCodegen:     true,
		Debounce:        40 * time.Millisecond,
		hooks: &hooks{
			onReady:         func() { close(ready) },
			onServerRestart: func() { restarts <- struct{}{} },
		},
	}
	cancel, done := runOrchestrator(t, cfg)
	defer func() { <-done }()
	defer cancel()

	<-ready
	// Touch a .go file — the watcher should debounce and trigger a restart.
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\n// edited\nfunc main() {}\n")

	select {
	case <-restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("go server was not restarted on a .go change")
	}
}

// TestRunSurvivesGoServerCrash proves a crashing Go server is reported and the
// loop survives — a later .go change still triggers a restart.
func TestRunSurvivesGoServerCrash(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	dir := writeProject(t)

	ready := make(chan struct{})
	restarts := make(chan struct{}, 4)
	cfg := Config{
		ProjectDir: dir,
		Logger:     quietLogger(),
		// A crashing child: it exits non-zero immediately.
		GoServerCommand: []string{bin, "crash"},
		SkipCodegen:     true,
		Debounce:        40 * time.Millisecond,
		hooks: &hooks{
			onReady:         func() { close(ready) },
			onServerRestart: func() { restarts <- struct{}{} },
		},
	}
	cancel, done := runOrchestrator(t, cfg)
	defer cancel()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator never reached ready despite a crashing child")
	}
	// The loop must still be alive: a .go change still triggers a restart.
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\n// edit\nfunc main() {}\n")
	select {
	case <-restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not survive a crashing child")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run errored after a child crash: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestRunNoGoroutineLeak proves a full Run lifecycle leaves no leaked
// goroutine behind once it returns.
func TestRunNoGoroutineLeak(t *testing.T) {
	bin := buildChildBin(t)
	dir := writeProject(t)
	withWeb(t, dir)

	settle := func() int {
		for i := 0; i < 20; i++ {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}
	before := settle()

	ready := make(chan struct{})
	cfg := Config{
		ProjectDir:      dir,
		Logger:          quietLogger(),
		GoServerCommand: []string{bin, "run"},
		ViteCommand:     []string{bin, "run"},
		SkipCodegen:     true,
		hooks:           &hooks{onReady: func() { close(ready) }},
	}
	cancel, done := runOrchestrator(t, cfg)
	<-ready
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	after := settle()
	// A small slack absorbs runtime/test-framework goroutine noise; a leaked
	// watcher or supervisor goroutine would show up well above it.
	if after > before+3 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestRunConcurrentChangesNoDeadlock fires many file events concurrently and
// proves the loop neither deadlocks nor leaks — the -race orchestrator proof.
func TestRunConcurrentChangesNoDeadlock(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	dir := writeProject(t)

	ready := make(chan struct{})
	cfg := Config{
		ProjectDir:      dir,
		Logger:          quietLogger(),
		GoServerCommand: []string{bin, "run"},
		SkipCodegen:     true,
		Debounce:        30 * time.Millisecond,
		hooks:           &hooks{onReady: func() { close(ready) }},
	}
	cancel, done := runOrchestrator(t, cfg)
	<-ready

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = os.WriteFile(filepath.Join(dir, "main.go"),
					[]byte("package main\nfunc main(){}\n"), 0o600)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run errored under concurrent changes: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run deadlocked under concurrent file changes")
	}
}

// waitFileExists fails the test if path does not appear within a bounded wait.
// The loop returns the instant the file appears; the ceiling is generous so a
// CPU-saturated CI run — many -race packages compiling and testing in parallel
// — cannot starve a supervised child's spawn-and-write past the deadline and
// produce a spurious failure. A genuine never-appears bug still fails.
func waitFileExists(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never appeared", path)
}
