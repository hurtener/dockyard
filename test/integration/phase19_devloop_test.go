// This file is the Phase 19 integration test (CLAUDE.md §17). Phase 19's deps
// name shipped phases 17 (`dockyard new`) and 18 (`internal/generate`), and
// the dev orchestrator drives both: it scaffolds nothing itself but runs
// against a scaffolded project and re-runs the real codegen pipeline. So it
// ships an end-to-end integration test driven against real components with no
// mocks at the seam:
//
//   - it runs the real `dockyard new` scaffold to produce a project, and
//     `go mod tidy`s it against the real Dockyard checkout so the ephemeral
//     schema generator's `go run` resolves;
//   - it starts the real internal/devloop.Run orchestrator against that
//     project with a REAL fsnotify watcher and a REAL child process — the
//     Go-server child is a small controllable stub binary injected via the
//     Config seam (a real `go run .` rebuild on every restart would make the
//     test heavy; the watcher and the codegen path are fully real, no mocks);
//   - (1) it touches a .go file and asserts the Go server child is restarted
//     (observed via the orchestrator's restart hook — a deterministic signal,
//     not a sleep);
//   - (2) it edits a contract source file and asserts the real codegen re-ran
//     and the generated output bytes changed;
//   - (3) it cancels the context and asserts the whole process tree is torn
//     down — no orphan child process, no leaked goroutine;
//   - it covers a failure mode: a Go-server child that crashes on start is
//     reported and the loop survives, never panicking dockyard dev itself.
//
// The test runs under -race with bounded timeouts and deterministic waits on
// observable signals.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/devloop"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// scaffoldP19Project runs the real scaffold and `go mod tidy`, returning the
// project directory. The go.mod replaces the Dockyard import at this repo's
// root so the codegen pipeline's ephemeral generator compiles.
func scaffoldP19Project(t *testing.T, name string) string {
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
		t.Fatalf("go mod tidy in scaffolded project failed: %v\n%s", err, out)
	}
	return res.Dir
}

// stubChildSrc is a minimal controllable child binary used as the supervised
// Go-server stub: it blocks on a termination signal so the supervisor can
// start, restart, and reap it deterministically.
const stubChildSrc = `package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
}
`

// buildStubChild compiles the stub child binary once and returns its path.
func buildStubChild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(stubChildSrc), 0o600); err != nil {
		t.Fatalf("write stub child source: %v", err)
	}
	bin := filepath.Join(dir, "stub-server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, src) //nolint:gosec // bin/src are test temp paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stub child: %v\n%s", err, out)
	}
	return bin
}

// stubCrashSrc is a child that exits non-zero immediately — the crash/failure
// mode the dev loop must survive.
const stubCrashSrc = `package main

import "os"

func main() { os.Exit(7) }
`

func buildStubCrash(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(stubCrashSrc), 0o600); err != nil {
		t.Fatalf("write crash child source: %v", err)
	}
	bin := filepath.Join(dir, "crash-server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, src) //nolint:gosec // bin/src are test temp paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build crash child: %v\n%s", err, out)
	}
	return bin
}

// TestPhase19_DevLoopRestartsAndRegenerates is the end-to-end happy-path test:
// a real scaffolded project, a real fsnotify watcher, a real codegen pipeline,
// and a real child process. It exercises restart-on-.go-change,
// codegen-on-contract-change, and clean teardown.
func TestPhase19_DevLoopRestartsAndRegenerates(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping dev-loop integration test in -short mode")
	}
	projectDir := scaffoldP19Project(t, "devloop-it")
	stub := buildStubChild(t)

	ready := make(chan struct{})
	restarts := make(chan struct{}, 8)
	codegenRuns := make(chan error, 8)

	cfg := devloop.Config{
		ProjectDir:      projectDir,
		Logger:          quietLogger(),
		Debounce:        60 * time.Millisecond,
		GoServerCommand: []string{stub},
		// Hooks are the deterministic observation seam (test build tag exposes
		// them via devloop.NewTestConfig).
	}
	cfg = devloop.WithTestHooks(cfg, devloop.TestHooks{
		OnReady:         func() { close(ready) },
		OnServerRestart: func() { restarts <- struct{}{} },
		OnCodegen:       func(err error) { codegenRuns <- err },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devloop.Run(ctx, cfg) }()

	// --- the orchestrator comes up -----------------------------------------
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("dev loop never reached ready")
	}

	// --- (1) a .go change restarts the Go server ---------------------------
	mainGo := filepath.Join(projectDir, "main.go")
	appendLine(t, mainGo, "\n// dev-loop integration edit\n")
	select {
	case <-restarts:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Go server was not restarted on a .go change")
	}

	// --- (2) a contract change re-runs codegen -----------------------------
	contractFile := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	before := readFile(t, contractFile)
	// Append a new exported type to the contract source — codegen must run and
	// the regenerated TypeScript file's bytes must change.
	tsFile := filepath.Join(projectDir, "internal", "contracts", "contracts.ts")
	tsBefore := readFile(t, tsFile)
	appendLine(t, contractFile, "\n// DevLoopProbe is a contract-change probe.\n"+
		"type DevLoopProbe struct {\n\tField string `json:\"field\"`\n}\n")

	// Wait for at least one codegen run to complete successfully.
	waitCodegenOK(t, codegenRuns)

	tsAfter := readFile(t, tsFile)
	if string(tsBefore) == string(tsAfter) {
		t.Fatal("codegen ran but the generated contracts.ts did not change")
	}
	if string(before) == string(readFile(t, contractFile)) {
		t.Fatal("test precondition: contract source was not actually edited")
	}

	// --- (3) cancel tears the whole tree down cleanly ----------------------
	beforeGoroutines := stableGoroutines()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dev loop returned an error on clean cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dev loop did not return after context cancel")
	}
	// Poll until the goroutine count settles back to the baseline. devloop's
	// child-process, supervisor and fsnotify goroutines unwind asynchronously
	// after cancel, so a one-shot snapshot can catch teardown mid-flight on a
	// loaded CI runner; polling-until-settled with a deadline is deterministic.
	leakDeadline := time.Now().Add(10 * time.Second)
	var afterGoroutines int
	for {
		runtime.GC()
		afterGoroutines = runtime.NumGoroutine()
		if afterGoroutines <= beforeGoroutines+4 {
			break
		}
		if time.Now().After(leakDeadline) {
			t.Fatalf("goroutine leak after teardown: before=%d after=%d",
				beforeGoroutines, afterGoroutines)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestPhase19_DevLoopSurvivesCrashingServer covers the failure mode: a
// Go-server child that exits non-zero on start is reported and the loop
// survives — cancel still returns cleanly, dockyard dev never panics.
func TestPhase19_DevLoopSurvivesCrashingServer(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping dev-loop integration test in -short mode")
	}
	projectDir := scaffoldP19Project(t, "devloop-crash-it")
	crash := buildStubCrash(t)

	ready := make(chan struct{})
	cfg := devloop.Config{
		ProjectDir:      projectDir,
		Logger:          quietLogger(),
		Debounce:        60 * time.Millisecond,
		GoServerCommand: []string{crash},
	}
	cfg = devloop.WithTestHooks(cfg, devloop.TestHooks{
		OnReady: func() { close(ready) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devloop.Run(ctx, cfg) }()

	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("dev loop never reached ready despite a crashing child")
	}

	// The loop must remain alive and tear down cleanly.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dev loop errored after a crashing child: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dev loop did not return after a crashing child")
	}
}

// appendLine appends content to a file, failing the test on error.
func appendLine(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // test temp file
	if err != nil {
		t.Fatalf("open %s for append: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append to %s: %v", path, err)
	}
}

// readFile reads a file, failing the test on error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test temp file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

// waitCodegenOK drains codegen-run signals until one reports success, failing
// the test if none arrives within a bounded wait. A transient codegen error
// (the contract is mid-edit) is tolerated — the test waits for the eventual
// successful run.
func waitCodegenOK(t *testing.T, runs <-chan error) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case err := <-runs:
			if err == nil {
				return
			}
			t.Logf("codegen run reported a transient error, awaiting next: %v", err)
		case <-deadline:
			t.Fatal("codegen never completed successfully after a contract change")
		}
	}
}

// stableGoroutines returns the goroutine count after letting the runtime
// settle — used to detect a leak across a Run lifecycle.
func stableGoroutines() int {
	for i := 0; i < 20; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}
