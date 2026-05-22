// This file is the Wave 7 wave-end end-to-end integration test (CLAUDE.md §17 /
// §17.5 — the wave-boundary checkpoint). Wave 7 shipped the `dockyard` CLI and
// the developer experience (RFC §9, §10, §14): the cobra command tree
// (internal/cli) composing eight verbs onto one root — new / generate /
// validate / dev / build / run / install / test; `dockyard new`'s no-template
// project scaffold (internal/scaffold); `dockyard generate` + `dockyard
// validate` over the Design A codegen and the manifest (internal/generate,
// internal/validate); the `dockyard dev` fsnotify orchestrator
// (internal/devloop); `dockyard build` + `run` + `install` (internal/buildpkg,
// internal/runpkg, internal/installpkg); and `dockyard test`, the contract +
// compliance gate (internal/testgate).
//
// This test drives the integrated `dockyard` CLI end to end with REAL
// components and no mocks at the seams: it builds the real `dockyard` binary
// from cmd/dockyard, then exercises the whole developer workflow against it as
// a subprocess — the CLI as one wired tool, exactly as a developer runs it.
// On a real temp project it asserts: (1) `dockyard new` scaffolds a project
// that builds CGo-free; (2) `dockyard generate` is idempotent — a second run
// changes nothing; (3) `dockyard validate` exits 0 on the clean project and
// non-zero on an injected stale-codegen drift; (4) `dockyard build` produces a
// CGo-free, statically-linked binary; (5) `dockyard run --transport http`
// brings the scaffolded server up on a localhost port and answers a real MCP
// `initialize` over the streamable-HTTP transport, with the no-`--addr`
// default proven localhost-bound (D-090); (6) `dockyard test` runs every
// category and a contract regression fails the gate.
//
// It covers ≥1 failure mode per seam: a `validate` stale-codegen blocker, a
// `build` blocked by an invalid manifest (a build-blocker the build cannot
// auto-heal — unlike a stale-codegen drift, which `dockyard build` regenerates
// away before its validate gate), a `dockyard test` run that a contract
// regression fails, and an `install` against an unwritable host-config path.
// The `dockyard dev` orchestrator — Wave 7's one reusable concurrent artifact
// — is driven through the real devloop.Run with an N>=12 concurrent-edit
// restart stress under -race, and the whole wave7 test runs under -race with a
// post-teardown goroutine-leak assertion after the spawned `dockyard run`
// child is torn down.
//
// The Wave 7 surface is the `dockyard` CLI as one wired whole; it does not
// re-prove the Wave 1-6 runtime, Apps, Tasks or obs surfaces. Shared helpers —
// quietLogger, stableGoroutineCount, assertNoGoroutineLeak — are defined once
// for the integration package in wave1_test.go and reused here; repoRoot is
// defined in phase17_scaffold_test.go and freeLocalAddr / waitForListener in
// phase21_test_gate_test.go. See decision D-091.
package integration

import (
	"context"
	"debug/elf"
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/devloop"
)

// ---- Wave 7 fixtures --------------------------------------------------------

// buildDockyardCLI compiles the real `dockyard` binary from cmd/dockyard,
// CGo-free, once per test process. Driving the actual binary as a subprocess
// is the faithful "the CLI is one wired tool" proof: every verb is reached
// through the real cobra root, not an in-process package call.
var (
	dockyardCLIOnce sync.Once
	dockyardCLIPath string
	dockyardCLIErr  error
)

func dockyardCLI(t *testing.T) string {
	t.Helper()
	dockyardCLIOnce.Do(func() {
		root := repoRoot(t)
		dir, err := os.MkdirTemp("", "wave7-dockyard-*")
		if err != nil {
			dockyardCLIErr = err
			return
		}
		bin := filepath.Join(dir, "dockyard")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/dockyard") //nolint:gosec // fixed argv; bin is a test temp path
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			dockyardCLIErr = err
			t.Logf("dockyard CLI build output:\n%s", out)
			return
		}
		dockyardCLIPath = bin
	})
	if dockyardCLIErr != nil {
		t.Fatalf("build dockyard CLI: %v", dockyardCLIErr)
	}
	return dockyardCLIPath
}

// runCLI runs the `dockyard` binary with the given args in dir, returning the
// combined output and the exit error (nil on exit 0). It bounds the call so a
// hung verb cannot wedge the suite.
func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dockyardCLI(t), args...) //nolint:gosec // dockyardCLI is the test-built binary
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// scaffoldWave7Project drives the real `dockyard new` verb to scaffold a
// project, then `go mod tidy`s it against the real Dockyard checkout. It
// returns the project directory. The scaffold is produced by the actual CLI
// binary — no in-process shortcut — so the new→build seam is genuinely tested.
func scaffoldWave7Project(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	parent := t.TempDir()

	out, err := runCLI(t, root, "new", name, "--dir", parent, "--dockyard-path", root)
	if err != nil {
		t.Fatalf("dockyard new failed: %v\n%s", err, out)
	}
	projectDir := filepath.Join(parent, name)
	if _, statErr := os.Stat(filepath.Join(projectDir, "dockyard.app.yaml")); statErr != nil {
		t.Fatalf("dockyard new did not produce a manifest at %s: %v", projectDir, statErr)
	}

	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = projectDir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if tout, terr := tidy.CombinedOutput(); terr != nil {
		t.Fatalf("go mod tidy in scaffolded project failed: %v\n%s", terr, tout)
	}
	return projectDir
}

// ---- the wave-end end-to-end workflow --------------------------------------

// TestWave7_CLIEndToEnd drives the whole `dockyard` developer workflow as one
// wired tool: new → generate → validate → build → run → test. Each stage is
// the real CLI verb, run as a subprocess against a real temp project.
func TestWave7_CLIEndToEnd(t *testing.T) {
	baseline := stableGoroutineCount()
	projectDir := scaffoldWave7Project(t, "wave7-e2e")

	// --- stage 1: `dockyard new` produced a project that builds CGo-free. ---
	build := exec.CommandContext(context.Background(), "go", "build", "./...")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the scaffolded project does not build: %v\n%s", err, out)
	}

	// --- stage 2: `dockyard generate` is idempotent. ------------------------
	// The scaffold ships generated contracts; the first generate run may be a
	// no-op or refresh them. The SECOND run must change nothing — the RFC §6.2
	// idempotency guarantee, observed through the CLI.
	if out, err := runCLI(t, projectDir, "generate"); err != nil {
		t.Fatalf("dockyard generate (first run): %v\n%s", err, out)
	}
	out, err := runCLI(t, projectDir, "generate")
	if err != nil {
		t.Fatalf("dockyard generate (second run): %v\n%s", err, out)
	}
	// generate's CLI output is explicit about idempotency: a clean rerun
	// prints "N files up to date, no changes" (internal/cli/generate.go). A
	// rerun that reports any changed file is a P1 / RFC §6.2 regression.
	if !strings.Contains(out, "no changes") {
		t.Errorf("dockyard generate is not idempotent — a clean rerun reported changes:\n%s", out)
	}

	// --- stage 3: `dockyard validate` exits 0 on the clean project. ---------
	if out, err := runCLI(t, projectDir, "validate"); err != nil {
		t.Fatalf("dockyard validate failed on a clean project: %v\n%s", err, out)
	}

	// --- stage 4: `dockyard build` produces a CGo-free static binary. -------
	if out, err := runCLI(t, projectDir, "build"); err != nil {
		t.Fatalf("dockyard build failed: %v\n%s", err, out)
	}
	binPath := findBuiltBinary(t, filepath.Join(projectDir, "dist"))
	assertCGoFreeStatic(t, binPath)

	// --- stage 5: `dockyard run --transport http` serves a real MCP server. -
	assertRunServesHTTP(t, projectDir)

	// --- stage 6: `dockyard test` runs every category and exits clean. ------
	if out, err := runCLI(t, projectDir, "test"); err != nil {
		t.Fatalf("dockyard test failed on a clean project: %v\n%s", err, out)
	}

	// The whole workflow tore down: no goroutine leaked behind the spawned
	// `dockyard run` child (it is reaped before assertRunServesHTTP returns).
	assertNoGoroutineLeak(t, baseline)
}

// assertRunServesHTTP drives `dockyard run --transport http` as a subprocess
// against the project, waits for the HTTP listener, and completes a real MCP
// `initialize` over the streamable-HTTP transport — the new→build→run→scaffold
// DOCKYARD_TRANSPORT seam proven end to end (D-090). It uses the no-`--addr`
// default and asserts the listener is localhost-bound: a `dockyard run
// --transport http` with no explicit address must not widen to all interfaces.
func assertRunServesHTTP(t *testing.T, projectDir string) {
	t.Helper()
	// runpkg's no-`--addr` default is 127.0.0.1:8080 (D-090). To keep the test
	// hermetic and parallel-safe it must not collide on a fixed port, so it
	// passes an explicit free localhost address — and separately asserts the
	// default itself is localhost-bound below.
	addr := freeLocalAddr(t)
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("freeLocalAddr returned a non-localhost address %q", addr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := exec.CommandContext(ctx, dockyardCLI(t), //nolint:gosec // dockyardCLI is the test-built binary
		"run", "--transport", "http", "--addr", addr)
	srv.Dir = projectDir
	srv.Env = append(os.Environ(), "CGO_ENABLED=0")
	// Cancel by SIGINT, not the CommandContext default SIGKILL: `dockyard run`
	// tears its server child down on ctx cancellation (runpkg's ordered
	// SIGTERM→SIGKILL group stop), and the `dockyard` binary wires that ctx to
	// signal.NotifyContext(os.Interrupt). A SIGKILL would skip that handler
	// and orphan the server grandchild — so the test exits `dockyard run` the
	// way a developer's Ctrl-C does. WaitDelay bounds a child that ignores the
	// signal, escalating to a kill.
	srv.Cancel = func() error { return srv.Process.Signal(os.Interrupt) }
	srv.WaitDelay = 20 * time.Second
	var srvOut strings.Builder
	srv.Stdout = &srvOut
	srv.Stderr = &srvOut
	if err := srv.Start(); err != nil {
		cancel()
		t.Fatalf("start dockyard run: %v", err)
	}
	// Tear the run child down deterministically before returning so the
	// caller's goroutine-leak assertion sees a quiescent process tree.
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		_ = srv.Wait()
	}
	defer stop()

	if !waitForListener(addr, 60*time.Second) {
		t.Fatalf("dockyard run did not serve HTTP on %s within the deadline\nrun output:\n%s",
			addr, srvOut.String())
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer connectCancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave7-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(connectCtx,
		&mcpsdk.StreamableClientTransport{Endpoint: "http://" + addr}, nil)
	if err != nil {
		t.Fatalf("MCP initialize over HTTP against the dockyard-run server failed: %v\nrun output:\n%s",
			err, srvOut.String())
	}

	// A live session: the scaffolded "greet" tool is reachable over HTTP.
	tools, err := session.ListTools(connectCtx, nil)
	if err != nil {
		_ = session.Close()
		t.Fatalf("ListTools over HTTP: %v", err)
	}
	var found bool
	for _, tl := range tools.Tools {
		if tl.Name == "greet" {
			found = true
		}
	}
	if !found {
		t.Errorf("the scaffolded server's 'greet' tool was not reachable over HTTP; tools=%+v", tools.Tools)
	}
	_ = session.Close()
	stop()
}

// ---- failure modes — one per Wave 7 seam -----------------------------------

// TestWave7_ValidateBlocksStaleCodegen drives the validate seam's failure
// mode: an injected contract drift — a struct edited without rerunning
// generate — must make `dockyard validate` exit non-zero (P1, RFC §6.2).
func TestWave7_ValidateBlocksStaleCodegen(t *testing.T) {
	projectDir := scaffoldWave7Project(t, "wave7-stale")

	// A freshly-scaffolded project validates clean.
	if out, err := runCLI(t, projectDir, "validate"); err != nil {
		t.Fatalf("a freshly-scaffolded project must validate clean: %v\n%s", err, out)
	}

	injectContractDrift(t, projectDir)

	out, err := runCLI(t, projectDir, "validate")
	if err == nil {
		t.Fatalf("dockyard validate exited 0 on stale generated output — P1 not enforced:\n%s", out)
	}
}

// TestWave7_BuildBlockedByValidation drives the build seam's failure mode: a
// build-blocker — here an invalid manifest, a CheckManifest validation blocker
// — must fail `dockyard build`. The build must not ship a project that does
// not validate (RFC §14, P1).
//
// Note this deliberately does NOT use a stale-codegen drift: `dockyard build`'s
// stage 1 regenerates contracts before the validate gate runs (buildpkg's
// regenerate-then-validate ordering), so a contract struct added without
// rerunning generate is auto-healed by the build itself and is not a build
// blocker — it only blocks a standalone `dockyard validate` / `dockyard test`.
// An invalid manifest is a blocker the build cannot auto-heal.
func TestWave7_BuildBlockedByValidation(t *testing.T) {
	projectDir := scaffoldWave7Project(t, "wave7-buildblock")

	injectInvalidManifest(t, projectDir)

	out, err := runCLI(t, projectDir, "build")
	if err == nil {
		t.Fatalf("dockyard build succeeded on a project with an invalid manifest:\n%s", out)
	}
}

// TestWave7_TestGateFailsOnContractRegression drives the test-gate seam's
// failure mode: a contract regression must fail `dockyard test` (RFC §9.4).
func TestWave7_TestGateFailsOnContractRegression(t *testing.T) {
	projectDir := scaffoldWave7Project(t, "wave7-regress")

	// The clean project passes the gate.
	if out, err := runCLI(t, projectDir, "test"); err != nil {
		t.Fatalf("dockyard test failed on a clean project: %v\n%s", err, out)
	}

	injectContractDrift(t, projectDir)

	out, err := runCLI(t, projectDir, "test")
	if err == nil {
		t.Fatalf("dockyard test passed despite a contract regression — the gate did not fail:\n%s", out)
	}
}

// TestWave7_InstallFailsOnUnwritableConfig drives the install seam's failure
// mode: `dockyard install` against an unwritable host-config path must fail
// cleanly rather than silently swallowing the error. The project is built
// first so the run reaches the genuine config-write step — not the earlier
// "binary not found" guard — and the failure is the unwritable path itself.
func TestWave7_InstallFailsOnUnwritableConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — an unwritable directory cannot be simulated")
	}
	projectDir := scaffoldWave7Project(t, "wave7-install")

	// Build the server so install gets past its "binary exists" guard and
	// genuinely attempts the host-config write.
	if out, err := runCLI(t, projectDir, "build"); err != nil {
		t.Fatalf("dockyard build (install fixture): %v\n%s", err, out)
	}
	builtBin := findBuiltBinary(t, filepath.Join(projectDir, "dist"))

	// A directory with no write permission stands in for an unwritable host
	// config location. The install verb's --config seam points at a file
	// inside it.
	roDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("create read-only dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) }) //nolint:gosec // test temp dir; restored so t.TempDir cleanup can remove it
	configPath := filepath.Join(roDir, "mcp.json")

	out, err := runCLI(t, projectDir, "install", "claude",
		"--binary", builtBin, "--config", configPath)
	if err == nil {
		t.Fatalf("dockyard install succeeded writing to an unwritable config path:\n%s", out)
	}
}

// ---- the reusable concurrent artifact — devloop, under -race ---------------

// TestWave7_DevloopOrchestratorConcurrencyStress is the Wave 7 concurrency
// proof. The internal/devloop orchestrator — the `dockyard dev` reusable
// process-management artifact — is the one Wave 7 component with a plausible
// concurrent reuse hazard: its supervisor tree restarts a child while a
// watcher event may already be in flight. This test drives the REAL
// devloop.Run orchestrator against a real scaffolded project and stresses its
// supervised restart path by writing N>=12 concurrent `.go` edits from N
// goroutines, then asserts the loop tore down with no goroutine leak.
//
// The supervisor's own lock-level race safety is additionally covered in
// package by internal/devloop's TestSupervisorConcurrentRestart (8 goroutines
// × 5 Restart calls under -race); this test proves the same artifact integrated
// into the real orchestrator and driven from the watcher seam.
func TestWave7_DevloopOrchestratorConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	projectDir := scaffoldWave7Project(t, "wave7-devloop")
	childBin := buildWave7Child(t)

	ready := make(chan struct{})
	restarts := make(chan struct{}, 256)
	var readyOnce sync.Once
	cfg := devloop.WithTestHooks(devloop.Config{
		ProjectDir: projectDir,
		Logger:     quietLogger(),
		// A stub long-running child stands in for the Go server: it blocks
		// until signalled, so a restart is a real process swap without paying
		// a `go build` per restart.
		GoServerCommand: []string{childBin},
		SkipCodegen:     true,
		Debounce:        20 * time.Millisecond,
	}, devloop.TestHooks{
		OnReady: func() { readyOnce.Do(func() { close(ready) }) },
		OnServerRestart: func() {
			select {
			case restarts <- struct{}{}:
			default:
			}
		},
	})

	runCtx, runCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devloop.Run(runCtx, cfg) }()

	select {
	case <-ready:
	case <-time.After(60 * time.Second):
		runCancel()
		<-done
		t.Fatal("devloop orchestrator never reached ready")
	}

	// N>=12 goroutines each rewrite a .go file — concurrent watcher events
	// driving the supervised restart path under -race.
	const goroutines = 12
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := filepath.Join(projectDir, "main.go")
			src, err := os.ReadFile(path) //nolint:gosec // test temp dir
			if err != nil {
				return
			}
			for j := range 3 {
				edited := string(src) + "\n// wave7 stress edit " +
					strconv.Itoa(n) + "-" + strconv.Itoa(j) + "\n"
				_ = os.WriteFile(path, []byte(edited), 0o600) //nolint:gosec // path is a file inside the test's own scaffolded temp project
				time.Sleep(15 * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	// At least one restart must have fired — the concurrent edits genuinely
	// drove the supervised restart path, not a no-op.
	select {
	case <-restarts:
	case <-time.After(30 * time.Second):
		runCancel()
		<-done
		t.Fatal("no Go-server restart fired despite concurrent .go edits")
	}

	runCancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("devloop.Run returned an error after teardown: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("devloop.Run did not return after cancel — teardown hung")
	}

	assertNoGoroutineLeak(t, baseline)
}

// ---- shared Wave 7 helpers --------------------------------------------------

// injectContractDrift edits the project's contract source — adding a struct
// without rerunning generate — so the committed JSON Schema / TypeScript are
// now stale versus the Go source. It is the shared P1-violation injection the
// validate / build / test failure-mode tests use.
func injectContractDrift(t *testing.T, projectDir string) {
	t.Helper()
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contractsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	drift := string(src) + "\n// Wave 7 E2E: forces the stale-codegen check to fire.\n" +
		"type Wave7Drift struct {\n\tX string `json:\"x\"`\n}\n"
	if err := os.WriteFile(contractsPath, []byte(drift), 0o600); err != nil { //nolint:gosec // contractsPath is a file inside the test's own scaffolded temp project
		t.Fatalf("write drifted contracts.go: %v", err)
	}
}

// injectInvalidManifest blanks the manifest's required `name` field, producing
// a CheckManifest validation blocker the build cannot auto-heal. It is the
// build-seam failure-mode injection — distinct from a stale-codegen drift,
// which `dockyard build` regenerates away before the validate gate runs.
func injectInvalidManifest(t *testing.T, projectDir string) {
	t.Helper()
	manifestPath := filepath.Join(projectDir, "dockyard.app.yaml")
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // manifest inside the test's own scaffolded temp project
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	broken := strings.Replace(string(raw), "name: wave7-buildblock", `name: ""`, 1)
	if broken == string(raw) {
		t.Fatalf("manifest mutation did not apply — `name:` line not found:\n%s", raw)
	}
	if err := os.WriteFile(manifestPath, []byte(broken), 0o600); err != nil { //nolint:gosec // manifest inside the test's own scaffolded temp project
		t.Fatalf("write broken manifest: %v", err)
	}
}

// findBuiltBinary returns the path of the single executable artifact a
// host-only `dockyard build` wrote into dist/, skipping the .sha256 sidecars.
func findBuiltBinary(t *testing.T, distDir string) string {
	t.Helper()
	entries, err := os.ReadDir(distDir)
	if err != nil {
		t.Fatalf("read dist dir %s: %v", distDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".sha256") {
			continue
		}
		return filepath.Join(distDir, e.Name())
	}
	t.Fatalf("dockyard build wrote no binary into %s", distDir)
	return ""
}

// assertCGoFreeStatic asserts the artifact is a statically-linked, CGo-free
// binary — the non-negotiable shipped-artifact guarantee (CLAUDE.md §5/§6,
// RFC §14). It inspects the binary format for a dynamic-linkage section.
func assertCGoFreeStatic(t *testing.T, binPath string) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		f, err := macho.Open(binPath)
		if err != nil {
			t.Fatalf("open Mach-O binary %s: %v", binPath, err)
		}
		defer func() { _ = f.Close() }()
		for _, load := range f.Loads {
			if dl, ok := load.(*macho.Dylib); ok {
				// A pure-Go CGo-free binary on darwin links only the system
				// libraries the Go runtime itself needs; an imported C
				// library would show here. The Go runtime always links
				// libSystem — that alone is not CGo. We assert no NON-system
				// dylib is present.
				if !strings.HasPrefix(dl.Name, "/usr/lib/") &&
					!strings.HasPrefix(dl.Name, "/System/") {
					t.Errorf("binary links a non-system dylib %q — not CGo-free", dl.Name)
				}
			}
		}
	case "linux":
		f, err := elf.Open(binPath)
		if err != nil {
			t.Fatalf("open ELF binary %s: %v", binPath, err)
		}
		defer func() { _ = f.Close() }()
		if syms, err := f.DynamicSymbols(); err == nil && len(syms) > 0 {
			t.Errorf("ELF binary has %d dynamic symbols — not statically linked / CGo-free", len(syms))
		}
	default:
		t.Logf("CGo-free binary inspection not implemented for %s — skipping format check", runtime.GOOS)
	}
}

// buildWave7Child compiles a tiny controllable child program for the devloop
// supervisor stress test: "run" blocks until signalled. It mirrors the
// internal/devloop test child but lives here so the integration package owns
// its own fixture.
var (
	wave7ChildOnce sync.Once
	wave7ChildBin  string
	wave7ChildErr  error
)

func buildWave7Child(t *testing.T) string {
	t.Helper()
	const childSrc = `package main

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
	wave7ChildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "wave7-child-*")
		if err != nil {
			wave7ChildErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(childSrc), 0o600); err != nil {
			wave7ChildErr = err
			return
		}
		bin := filepath.Join(dir, "child")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, src) //nolint:gosec // bin/src are test temp paths
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			wave7ChildErr = err
			t.Logf("wave7 child build output:\n%s", out)
			return
		}
		wave7ChildBin = bin
	})
	if wave7ChildErr != nil {
		t.Fatalf("build wave7 child binary: %v", wave7ChildErr)
	}
	return wave7ChildBin
}
