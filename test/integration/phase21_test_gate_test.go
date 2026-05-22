// This file is the Phase 21 integration test (CLAUDE.md §17). Phase 21's deps
// name shipped Phase 18 and it consumes internal/validate, internal/codegen,
// internal/generate and runtime/server — so it ships an end-to-end integration
// test driven against real components with no mocks at the seam:
//
//   - it runs the real `dockyard new` scaffold to produce a project and
//     `go mod tidy`s it against the real Dockyard checkout;
//   - it runs the real internal/testgate.Run gate across the clean project and
//     asserts every category passes and the run exits clean (Failed() == false);
//   - it introduces a contract regression — a contract struct edited without
//     regenerating — and asserts the contract category and the gate fail;
//   - it introduces a spec-compliance violation — a missing vendored spec — and
//     asserts the spec-compliance category and the gate fail;
//   - it breaks a project test and asserts the go-test category fails (the ≥1
//     additional failure mode CLAUDE.md §17 requires);
//   - it builds the scaffolded server and proves it honours
//     DOCKYARD_TRANSPORT=http by running it as a subprocess and completing a
//     real MCP initialize over the streamable-HTTP transport — the Phase 20↔17
//     wiring-gap fix, closed end to end.
//
// The test runs under -race.
package integration

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/internal/testgate"
)

// scaffoldP21Project runs the real scaffold, `go mod tidy`s it against this
// repo's root, and runs the real generate pipeline — a clean, fully-generated
// project the gate runs against.
func scaffoldP21Project(t *testing.T, name string) string {
	t.Helper()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             t.TempDir(),
		DockyardReplace: repoRoot(t),
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
	m, err := manifest.LoadFile(filepath.Join(res.Dir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if _, err := generate.Run(generate.Options{ProjectDir: res.Dir, Manifest: m}); err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	return res.Dir
}

// categoryResult returns the named category's Result, or fails the test.
func categoryResult(t *testing.T, rep *testgate.Report, c testgate.Category) testgate.Result {
	t.Helper()
	for _, res := range rep.Results {
		if res.Category == c {
			return res
		}
	}
	t.Fatalf("report has no %q category:\n%s", c, renderReport(rep))
	return testgate.Result{}
}

func renderReport(rep *testgate.Report) string {
	var b strings.Builder
	for _, res := range rep.Results {
		b.WriteString("  ")
		b.WriteString(res.String())
		b.WriteString("\n")
	}
	return b.String()
}

// TestPhase21_GatePassesCleanProject runs the full gate against a real, clean
// scaffolded project and asserts every category passes.
func TestPhase21_GatePassesCleanProject(t *testing.T) {
	t.Parallel()
	dir := scaffoldP21Project(t, "p21-clean")

	rep, err := testgate.Run(testgate.Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("testgate.Run: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("the gate failed on a clean project:\n%s", renderReport(rep))
	}
	for _, cat := range []testgate.Category{
		testgate.CategoryGoTest,
		testgate.CategoryContract,
		testgate.CategoryGolden,
		testgate.CategorySpecCompliance,
		testgate.CategoryCapability,
	} {
		if res := categoryResult(t, rep, cat); !res.Passed {
			t.Errorf("category %q failed on a clean project: %s", cat, res.Detail)
		}
	}
}

// TestPhase21_ContractRegressionFailsTheGate edits a contract struct without
// regenerating — the committed schema/TS is now stale. The contract category
// and the gate must fail.
func TestPhase21_ContractRegressionFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldP21Project(t, "p21-contract")

	contracts := filepath.Join(dir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contracts) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	drift := string(src) + "\n// Drift forces the contract category to fire.\n" +
		"type Drift struct {\n\tX string `json:\"x\"`\n}\n"
	if err := os.WriteFile(contracts, []byte(drift), 0o600); err != nil { //nolint:gosec // contracts is under a test temp dir
		t.Fatalf("write drifted contracts.go: %v", err)
	}

	rep, err := testgate.Run(testgate.Options{ProjectDir: dir, SkipGoTest: true})
	if err != nil {
		t.Fatalf("testgate.Run: %v", err)
	}
	if !rep.Failed() {
		t.Fatalf("a contract regression did not fail the gate:\n%s", renderReport(rep))
	}
	if res := categoryResult(t, rep, testgate.CategoryContract); res.Passed {
		t.Errorf("the contract category passed despite a contract regression: %s", res.Detail)
	}
}

// TestPhase21_SpecComplianceViolationFailsTheGate withholds one vendored spec
// from a project carrying a docs/specifications/ tree — validate's CheckSpec
// reports the missing spec as a Blocker, which the spec-compliance category
// surfaces as a gating failure.
func TestPhase21_SpecComplianceViolationFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldP21Project(t, "p21-spec")

	specsDir := filepath.Join(dir, "docs", "specifications")
	if err := os.MkdirAll(specsDir, 0o750); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	// One spec present, the other withheld — the spec-compliance regression.
	if err := os.WriteFile(filepath.Join(specsDir, "mcp-apps-2026-01-26.mdx"),
		[]byte("vendored spec snapshot\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	rep, err := testgate.Run(testgate.Options{ProjectDir: dir, SkipGoTest: true})
	if err != nil {
		t.Fatalf("testgate.Run: %v", err)
	}
	if !rep.Failed() {
		t.Fatalf("a spec-compliance violation did not fail the gate:\n%s", renderReport(rep))
	}
	if res := categoryResult(t, rep, testgate.CategorySpecCompliance); res.Passed {
		t.Errorf("the spec-compliance category passed despite a missing vendored spec: %s", res.Detail)
	}
}

// TestPhase21_GoTestFailureFailsTheGate breaks a project's own contract test —
// the go-test category must fail (the ≥1 additional failure mode, §17).
func TestPhase21_GoTestFailureFailsTheGate(t *testing.T) {
	t.Parallel()
	dir := scaffoldP21Project(t, "p21-gotest")

	failing := "package main\n\nimport \"testing\"\n\n" +
		"func TestAlwaysFails(t *testing.T) { t.Fatal(\"intentional failure\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "greet_test.go"), []byte(failing), 0o600); err != nil {
		t.Fatalf("write failing test: %v", err)
	}

	rep, err := testgate.Run(testgate.Options{ProjectDir: dir})
	if err != nil {
		t.Fatalf("testgate.Run: %v", err)
	}
	if res := categoryResult(t, rep, testgate.CategoryGoTest); res.Passed {
		t.Errorf("the go-test category passed despite a failing project test: %s", res.Detail)
	}
	if !rep.Failed() {
		t.Errorf("a failing project test did not fail the gate")
	}
}

// TestPhase21_ScaffoldHonoursHTTPTransport is the Phase 20↔17 wiring-gap fix,
// proven end to end: it builds a scaffolded server, runs it as a subprocess
// with DOCKYARD_TRANSPORT=http, and completes a real MCP initialize over the
// streamable-HTTP transport. Before the fix the scaffold served stdio
// unconditionally and ignored DOCKYARD_TRANSPORT, so this would never connect.
func TestPhase21_ScaffoldHonoursHTTPTransport(t *testing.T) {
	t.Parallel()
	dir := scaffoldP21Project(t, "p21-http")

	// Build the scaffolded server, CGo-free.
	bin := filepath.Join(t.TempDir(), "p21-http-server")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", bin, ".") //nolint:gosec // fixed argv; bin is a test temp path
	build.Dir = dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scaffolded server: %v\n%s", err, out)
	}

	// Pick a free localhost port for the HTTP transport.
	addr := freeLocalAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := exec.CommandContext(ctx, bin) //nolint:gosec // bin is a test-built binary
	srv.Env = append(os.Environ(),
		"DOCKYARD_TRANSPORT=http",
		"DOCKYARD_HTTP_ADDR="+addr,
	)
	var srvOut strings.Builder
	srv.Stdout = &srvOut
	srv.Stderr = &srvOut
	if err := srv.Start(); err != nil {
		t.Fatalf("start scaffolded server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = srv.Wait()
	})

	// Wait for the HTTP listener to come up.
	endpoint := "http://" + addr
	if !waitForListener(addr, 10*time.Second) {
		t.Fatalf("scaffolded server did not start serving HTTP on %s\nserver output:\n%s",
			addr, srvOut.String())
	}

	// Complete a real MCP initialize over the streamable-HTTP transport — the
	// genuine proof the server honours DOCKYARD_TRANSPORT=http.
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer connectCancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "phase21-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(connectCtx,
		&mcpsdk.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("MCP initialize over HTTP against the scaffolded server failed: %v\nserver output:\n%s",
			err, srvOut.String())
	}
	defer func() { _ = session.Close() }()

	// The scaffold registers the example "greet" tool — list it to prove the
	// session is genuinely live over HTTP, not merely connected.
	tools, err := session.ListTools(connectCtx, nil)
	if err != nil {
		t.Fatalf("ListTools over HTTP: %v", err)
	}
	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "greet" {
			found = true
		}
	}
	if !found {
		t.Errorf("the scaffolded server's 'greet' tool was not reachable over HTTP; tools=%+v", tools.Tools)
	}
}

// freeLocalAddr returns a currently-free 127.0.0.1 address (host:port).
func freeLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a free port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// waitForListener polls addr until a TCP connection succeeds or the deadline
// passes.
func waitForListener(addr string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
