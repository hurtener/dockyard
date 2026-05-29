// This file is the Wave 8 wave-end end-to-end integration test (CLAUDE.md §17 /
// §17.5 — the wave-boundary checkpoint). Wave 8 shipped the local inspector
// (RFC §12): the `internal/inspector` localhost HTTP backend (the loopback
// gate, the read-only obs/v1 SSE relay, the bounded JSON-RPC log, the
// `/api/verdicts` and `/api/contracts` sources); the `web/inspector` Svelte
// frontend (the host half of the `ui/` bridge, the App preview frame, the
// Events / RPC / Fixtures / Tools / Verdicts / Tasks DetailRail panels, the
// Host capability-set control); and the standalone `dockyard inspect` CLI verb
// (`--url`, `--port`, `--no-open`).
//
// This test drives the integrated inspector end to end with REAL components and
// no mocks at the seams: a real `runtime/server` serving a real `runtime/apps`
// MCP App with a real contract-first tool, a real `runtime/obs` SSE sink behind
// the emitter seam, and a real `runtime/tasks` engine. It brings up the real
// `dockyard inspect` CLI binary as a subprocess — exactly as a developer runs
// it — attached to the running server's obs/v1 stream. It asserts: (1) `dockyard
// inspect --url` attaches to the running server and serves the inspector on a
// loopback port; (2) the inspector's read-only relay streams real obs/v1 events
// from real tool calls to a UI client (P2 — the inspector is a pure obs/v1
// client); (3) a real task lifecycle's `task.progress` obs/v1 events reach the
// inspector's stream (the surface the Tasks panel renders); (4) the
// `/api/verdicts` endpoint surfaces a real `internal/validate.Run` result, and
// `/api/contracts` surfaces the generated tool contract the fixture switcher
// derives fixtures from (P1); (5) the host-half bridge's `ui/initialize`
// handshake — including capability-set emulation degrading an App — is exercised
// against the real `dockyard-bridge` View half (driven by the `web/inspector`
// Vitest suite — see the note below); (6) a non-localhost `--port` bind is
// refused before any listener opens (P4, the CVE-2025-49596 lesson).
//
// It covers ≥1 failure mode per seam: a non-loopback `dockyard inspect --port`
// rejection, a `--url` with no host rejected, and a verdicts source over a
// project with no manifest surfacing a real Blocker verdict rather than a void.
// The inspector relay — Wave 8's reusable concurrent artifact — is stressed with
// N>=12 concurrent UI stream clients on one relay under -race, and the whole
// wave8 test runs under -race with a post-teardown goroutine-leak assertion.
// The leak baseline is read AFTER the in-process server/sink/session fixture is
// up and quiescent (those long-lived fixture goroutines are torn down later, by
// t.Cleanup) so the assertion targets exactly the artifact under test — the
// inspector backend, its relay's SSE-client loop, and the per-UI-client HTTP
// goroutines — and uses wave1_test.go's robust poll-until-settled
// assertNoGoroutineLeak, never a one-shot snapshot (D-102).
//
// Scope note. The host-half ↔ View-half `ui/initialize` handshake is a
// browser-side postMessage seam: the host half (`web/inspector/src/host/
// host-bridge.ts`) and the real `dockyard-bridge` View half complete a real
// handshake over a `MessageChannel` in the `web/inspector` Vitest suite
// (`host-bridge.test.ts` — "no mock at the protocol seam"), which `make web`
// runs in the same checkpoint gate. The Go wave8 surface is the inspector
// *backend* as one wired tool — the obs relay, the verdict/contract endpoints,
// and the `dockyard inspect` CLI — driven exactly as `dockyard inspect` runs
// it. This mirrors Wave 7, whose Go E2E drives the CLI surface while the
// frontend seams are proven by `make web`.
//
// Shared helpers — quietLogger, stableGoroutineCount, assertNoGoroutineLeak —
// are defined once for the integration package in wave1_test.go; waitConnect /
// waitFor / scanForEvent in phase22_inspector_test.go; ptrInt64 in
// phase13_tasks_test.go; dockyardCLI / runCLI in wave7_test.go; freeLocalAddr /
// waitForListener in phase21_test_gate_test.go. See decision D-102.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/inspector"
	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ---- Wave 8 fixtures --------------------------------------------------------

// wave8Input / wave8Output are the integration tool's contract-first structs —
// the real Go types `dockyard generate` would derive a JSON Schema and the
// inspector's fixture switcher its six fixtures from (P1).
type wave8Input struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type wave8Output struct {
	Region string `json:"region"`
	Total  int    `json:"total"`
}

func wave8Handler(_ context.Context, in wave8Input) (tool.Result[wave8Output], error) {
	return tool.Result[wave8Output]{
		Text:       "region " + in.Region + " reported",
		Structured: wave8Output{Region: in.Region, Total: 42},
	}, nil
}

// wave8AppHTML is the minimal MCP App the server registers — a ui:// resource
// the inspector previews in its App frame.
const wave8AppHTML = `<!doctype html><html><head><title>Wave 8 App</title>` +
	`</head><body><div id="app">wave-8 app</div></body></html>`

// wave8Contracts is the generated-contract JSON the inspector's `/api/contracts`
// source serves — the array the fixture switcher derives its six fixtures from
// (P1, RFC §6/§12). It is shaped exactly as the inspector frontend's contract
// model decodes: `{name, description, inputSchema, outputSchema}`.
const wave8Contracts = `[{"name":"report","description":"region report",` +
	`"inputSchema":{"type":"object","properties":{"region":{"type":"string"}}},` +
	`"outputSchema":{"type":"object","properties":` +
	`{"region":{"type":"string"},"total":{"type":"integer"}}}}]`

// wave8Server stands up a real runtime/server emitting obs/v1 to a real SSE
// sink, with a real contract-first tool and a real runtime/apps App registered.
// It returns the sink, the live MCP client session, and a tasks engine wired to
// the same obs stream — every seam real, no mock. Teardown is registered on t.
func wave8Server(t *testing.T) (*obs.SSESink, *mcpsdk.ClientSession, *tasks.Engine) {
	t.Helper()

	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	srv, err := server.New(
		server.Info{Name: "wave8-server", Version: "0.8.0"},
		&server.Options{Obs: sink, Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[wave8Input, wave8Output]("report").
		Describe("region report").
		Handler(wave8Handler)
	if err := report.Register(srv); err != nil {
		t.Fatalf("register report tool: %v", err)
	}
	if err := apps.Register(srv, apps.App{
		URI:  "ui://wave8/app",
		Name: "wave8-app",
		HTML: []byte(wave8AppHTML),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "wave8-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		Logger:   quietLogger(),
		Obs:      sink, // the same obs/v1 stream the inspector relays
		ServerID: "wave8-server",
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	return sink, session, engine
}

// ---- the wave-end end-to-end workflow --------------------------------------

// TestWave8_InspectorEndToEnd drives the integrated inspector as one wired tool:
// a real server + App + obs sink + tasks engine, the real inspector backend
// attached the `dockyard inspect --url` way, and a real UI client of the
// inspector's read-only relay. It asserts the whole inspector works end to end —
// attach, obs stream, task lifecycle, verdicts, contracts, App preview.
func TestWave8_InspectorEndToEnd(t *testing.T) {
	sink, session, engine := wave8Server(t)

	// The baseline is read AFTER the server/sink/session fixture is up and
	// quiescent: the leak assertion targets the artifact under test — the
	// inspector backend and its relay's SSE-client loop — not the long-lived
	// fixture goroutines (those are torn down by t.Cleanup, after this assert).
	baseline := stableGoroutineCount()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// The real inspector backend, attached to the running server's obs/v1
	// stream — the `dockyard inspect --url` configuration: a read-only relay,
	// a real verdict source, and the generated-contract source the fixture
	// switcher derives its six fixtures from (P1).
	relay := inspector.NewRelay("http://" + sink.Addr() + "/obs/v1/stream")
	t.Cleanup(func() { _ = relay.Close() })
	go relay.Run(ctx)

	insp, err := inspector.New(inspector.Options{
		Addr:  "127.0.0.1:0",
		Relay: relay,
		ServerInfo: inspector.ServerInfo{
			Name: "wave8-server", Version: "0.8.0", Transport: "http",
		},
		// A real verdict source: internal/validate.Run over a directory with no
		// manifest yields a real Blocker — the failure surfaces as a verdict,
		// never a void (RFC §12).
		Verdicts: inspector.VerdictsFromValidate(t.TempDir()),
		// The generated tool contracts the fixture switcher reads (P1).
		Contracts: func() json.RawMessage {
			return json.RawMessage(wave8Contracts)
		},
	})
	if err != nil {
		t.Fatalf("inspector.New: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
	go func() { _ = insp.Serve(ctx) }()

	// --- (1) attach: a UI client connects to the inspector's obs relay. ------
	streamReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet, insp.URL()+"/api/obs/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	streamResp, err := waitConnect(t, streamReq)
	if err != nil {
		t.Fatalf("connect inspector obs relay: %v", err)
	}
	t.Cleanup(func() { _ = streamResp.Body.Close() })
	waitFor(t, func() bool { return sink.Subscribers() > 0 },
		"inspector relay subscribes to the running server's obs sink")

	// --- (2) the relay streams real obs/v1 events from real tool calls. ------
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "emea"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !scanForEvent(t, streamResp, `"kind":"tool.call"`) {
		t.Fatal("inspector relay did not deliver a tool.call obs/v1 event from the running server")
	}

	// --- (3) a real task lifecycle reaches the inspector's stream. -----------
	// The Tasks panel folds task.progress obs/v1 events into the five-status
	// lifecycle; a real tasks.Engine driving a task to a terminal status must
	// emit them to the same obs stream the inspector relays.
	taskDone := make(chan struct{})
	if _, err := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "report",
		TaskMeta: protocolcodec.TaskMeta{TTL: ptrInt64(60000)},
		Run: func(context.Context) (json.RawMessage, error) {
			defer close(taskDone)
			return json.RawMessage(`{"ok":true}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	<-taskDone
	if !scanForEvent(t, streamResp, `"kind":"task.progress"`) {
		t.Fatal("inspector relay did not deliver a task.progress obs/v1 event")
	}

	// --- (4) the verdicts + contracts endpoints surface real results. --------
	verdicts := wave8GetVerdicts(t, insp.URL()+"/api/verdicts")
	if len(verdicts) == 0 {
		t.Fatal("Verdicts endpoint returned no verdicts for a real validate run")
	}
	sawError := false
	for _, v := range verdicts {
		if v.Severity == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected a real validate blocker verdict, got: %+v", verdicts)
	}
	contractsBody := wave8GetBody(t, insp.URL()+"/api/contracts")
	if !strings.Contains(contractsBody, `"report"`) {
		t.Fatalf("/api/contracts did not surface the generated contract: %s", contractsBody)
	}

	// --- (5) the App resource is served — the inspector previews it. ---------
	readResp, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "ui://wave8/app",
	})
	if err != nil {
		t.Fatalf("ReadResource ui://wave8/app: %v", err)
	}
	if len(readResp.Contents) == 0 ||
		!strings.Contains(readResp.Contents[0].Text, "wave-8 app") {
		t.Fatalf("App resource body unexpected: %+v", readResp.Contents)
	}

	// Teardown: cancel the context, close the inspector, and assert no
	// goroutine leaked behind the relay's SSE-client loop or the HTTP server.
	cancel()
	_ = streamResp.Body.Close()
	_ = insp.Close()
	_ = relay.Close()
	assertNoGoroutineLeak(t, baseline)
}

// TestWave8_InspectCLIAttachesToRunningServer drives the standalone `dockyard
// inspect` CLI binary as a subprocess — exactly as a developer runs it —
// attached to a running server's obs/v1 stream. It is the `dockyard inspect`
// seam (D-099): the CLI is one wired tool, reached through the real cobra root,
// not an in-process package call.
func TestWave8_InspectCLIAttachesToRunningServer(t *testing.T) {
	sink, session, _ := wave8Server(t)

	// The baseline is read after the in-process server fixture is quiescent —
	// the leak assertion targets this process's HTTP-client / stream goroutines
	// (the `dockyard inspect` inspector runs in its own subprocess, reaped by
	// stop()), not the long-lived fixture goroutines torn down by t.Cleanup.
	baseline := stableGoroutineCount()

	// The inspector serves on a free loopback port so the subprocess test is
	// hermetic and parallel-safe.
	inspectorAddr := freeLocalAddr(t)
	portIdx := strings.LastIndex(inspectorAddr, ":")
	inspectorPort := inspectorAddr[portIdx+1:]

	// `dockyard inspect --url <server> --port <p> --no-open` — the standalone
	// inspector verb. --url names the running server; the inspector derives the
	// canonical /obs/v1/stream path and relays it. --no-open suppresses the
	// browser-open for CI.
	runCtx, runCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var inspectOut strings.Builder
	go func() {
		defer close(done)
		out, _ := runCLICtx(runCtx, t, t.TempDir(),
			"inspect",
			"--url", "http://"+sink.Addr(),
			"--port", inspectorPort,
			"--no-open")
		inspectOut.WriteString(out)
	}()
	// Tear the inspect child down deterministically before the leak assertion.
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		runCancel()
		<-done
	}
	defer stop()

	if !waitForListener(inspectorAddr, 30*time.Second) {
		runCancel()
		<-done
		t.Fatalf("dockyard inspect did not serve on %s within the deadline\noutput:\n%s",
			inspectorAddr, inspectOut.String())
	}

	// A real UI client connects to the inspect-served inspector's obs relay.
	ctx := context.Background()
	streamReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://"+inspectorAddr+"/api/obs/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	streamResp, err := waitConnect(t, streamReq)
	if err != nil {
		t.Fatalf("connect inspect obs relay: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	waitFor(t, func() bool { return sink.Subscribers() > 0 },
		"dockyard inspect relay subscribes to the running server")

	// A real tool call — the inspect-served relay must deliver its obs/v1 event.
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "apac"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !scanForEvent(t, streamResp, `"kind":"tool.call"`) {
		t.Fatal("dockyard inspect relay did not deliver a tool.call obs/v1 event")
	}

	// The /api/info endpoint surfaces the attached server's read-only identity.
	infoBody := wave8GetBody(t, "http://"+inspectorAddr+"/api/info")
	if !strings.Contains(infoBody, `"transport":"http"`) {
		t.Fatalf("dockyard inspect /api/info unexpected: %s", infoBody)
	}

	_ = streamResp.Body.Close()
	stop()
	assertNoGoroutineLeak(t, baseline)
}

// ---- failure modes — one per Wave 8 seam -----------------------------------

// TestWave8_InspectorRefusesNonLoopback drives the P4 / RFC §12 failure mode:
// the inspector — the lone client-shaped surface — is never reachable
// off-localhost. A non-loopback or wildcard bind is refused before the listener
// opens (the CVE-2025-49596 lesson). This is the inspector backend's failure
// mode and the `dockyard inspect --port` seam's failure mode at once.
func TestWave8_InspectorRefusesNonLoopback(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"0.0.0.0:0", "192.168.1.10:0", ":0", "8.8.8.8:0",
	} {
		insp, err := inspector.New(inspector.Options{Addr: addr})
		if err == nil {
			_ = insp.Close()
			t.Fatalf("inspector.New(%q): want ErrNonLoopbackBind, got nil", addr)
		}
	}
	// A loopback bind is accepted — the inspector still serves on localhost.
	insp, err := inspector.New(inspector.Options{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("inspector.New(loopback): %v", err)
	}
	_ = insp.Close()
}

// TestWave8_InspectCLIRejectsBadURL drives the `dockyard inspect --url` seam's
// failure mode: a `--url` that is not a usable http(s) server URL is a typed,
// clean refusal — the inspector never attaches to a garbage stream.
func TestWave8_InspectCLIRejectsBadURL(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-a-url",       // no scheme
		"ftp://127.0.0.1", // wrong scheme
		"http://",         // no host
	} {
		out, err := runCLI(t, t.TempDir(), "inspect", "--url", bad, "--no-open")
		if err == nil {
			t.Fatalf("dockyard inspect --url %q: want a typed refusal, got success\n%s",
				bad, out)
		}
	}
}

// TestWave8_VerdictsSurfaceRealFailure drives the verdicts seam's failure mode:
// a verdict source rooted at a project with no manifest must surface a real
// Blocker verdict rather than a blank panel — the panel degrades gracefully,
// never a void (RFC §12).
func TestWave8_VerdictsSurfaceRealFailure(t *testing.T) {
	t.Parallel()
	insp, err := inspector.New(inspector.Options{
		Addr:     "127.0.0.1:0",
		Verdicts: inspector.VerdictsFromValidate(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("inspector.New: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = insp.Serve(ctx) }()

	var verdicts []inspector.Verdict
	waitFor(t, func() bool {
		verdicts = wave8GetVerdictsSoft(insp.URL() + "/api/verdicts")
		return len(verdicts) > 0
	}, "verdicts endpoint answers")
	sawError := false
	for _, v := range verdicts {
		if v.Severity == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("a project with no manifest must surface an error verdict, got: %+v", verdicts)
	}
}

// ---- the reusable concurrent artifact — the relay, under -race -------------

// TestWave8_RelayConcurrencyStress is the Wave 8 concurrency proof. The
// inspector relay — the read-only obs/v1 fan-out — is Wave 8's one reusable
// concurrent artifact: one upstream SSE stream fans out to every connected
// inspector UI client, and a slow client must never stall the relay (CLAUDE.md
// §8). This test drives the REAL relay against a REAL obs SSE sink fed by a
// REAL server, attaches N>=12 concurrent UI stream clients, fires real tool
// calls, and asserts every client receives the obs/v1 events — then tears the
// whole thing down and asserts no goroutine leaked.
//
// The relay's own lock-level race safety is additionally covered in package by
// internal/inspector's relay concurrency test; this proves the same artifact
// integrated against a real obs sink and many real HTTP UI clients.
func TestWave8_RelayConcurrencyStress(t *testing.T) {
	sink, session, _ := wave8Server(t)

	// Baseline read after the server fixture is quiescent: the leak assertion
	// targets the relay's fan-out goroutines and the N UI-client HTTP
	// goroutines — the artifact under test — not the fixture's own goroutines.
	baseline := stableGoroutineCount()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	relay := inspector.NewRelay("http://" + sink.Addr() + "/obs/v1/stream")
	go relay.Run(ctx)

	insp, err := inspector.New(inspector.Options{Addr: "127.0.0.1:0", Relay: relay})
	if err != nil {
		t.Fatalf("inspector.New: %v", err)
	}
	go func() { _ = insp.Serve(ctx) }()

	// N>=12 concurrent UI stream clients on one relay.
	const clients = 12
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		gotCount int
		bodies   = make([]*http.Response, 0, clients)
	)
	for i := range clients {
		req, reqErr := http.NewRequestWithContext(
			ctx, http.MethodGet, insp.URL()+"/api/obs/stream", nil)
		if reqErr != nil {
			t.Fatalf("client %d new request: %v", i, reqErr)
		}
		resp, connErr := waitConnect(t, req)
		if connErr != nil {
			t.Fatalf("client %d connect: %v", i, connErr)
		}
		bodies = append(bodies, resp)
	}
	t.Cleanup(func() {
		for _, b := range bodies {
			_ = b.Body.Close()
		}
	})
	// Each client scans its own stream for the obs/v1 tool.call event.
	for i := range clients {
		wg.Add(1)
		go func(resp *http.Response) {
			defer wg.Done()
			sc := bufio.NewScanner(resp.Body)
			deadline := time.Now().Add(15 * time.Second)
			for sc.Scan() {
				if strings.Contains(sc.Text(), `"kind":"tool.call"`) {
					mu.Lock()
					gotCount++
					mu.Unlock()
					return
				}
				if time.Now().After(deadline) {
					return
				}
			}
		}(bodies[i])
	}

	// Every client must be registered upstream before the tool calls fire.
	waitFor(t, func() bool { return relay.Subscribers() >= clients },
		"all UI clients subscribed to the relay")

	// Fire a burst of real tool calls so every client sees a tool.call event.
	for range 5 {
		if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "report",
			Arguments: map[string]any{"region": "emea"},
		}); err != nil {
			t.Fatalf("CallTool: %v", err)
		}
	}

	wg.Wait()
	mu.Lock()
	got := gotCount
	mu.Unlock()
	if got != clients {
		t.Fatalf("relay fan-out: %d of %d UI clients received a tool.call event", got, clients)
	}

	// Teardown: cancel, close, and assert the relay's SSE-client loop and every
	// per-client HTTP goroutine unwound — no goroutine leaked.
	cancel()
	for _, b := range bodies {
		_ = b.Body.Close()
	}
	_ = insp.Close()
	_ = relay.Close()
	assertNoGoroutineLeak(t, baseline)
}

// ---- shared Wave 8 helpers --------------------------------------------------

// runCLICtx runs the `dockyard` binary under a caller-supplied context so a
// long-running verb (`dockyard inspect`) can be cancelled deterministically. It
// is the cancellable sibling of wave7_test.go's runCLI: the binary is the same
// test-built `dockyard` from cmd/dockyard (dockyardCLI), reached as a
// subprocess through the real cobra root. Cancelling ctx tears the inspect
// child down (the `dockyard` binary wires ctx to the inspector's Serve).
func runCLICtx(ctx context.Context, t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, dockyardCLI(t), args...) //nolint:gosec // dockyardCLI is the test-built binary
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	// `dockyard inspect` stops cleanly on SIGINT (its ctx is wired to the
	// inspector's Serve / Close); a SIGKILL would skip that drain. WaitDelay
	// bounds a child that ignores the signal.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// wave8GetVerdicts GETs the inspector's verdicts endpoint and decodes the rows.
func wave8GetVerdicts(t *testing.T, url string) []inspector.Verdict {
	t.Helper()
	body := wave8GetBody(t, url)
	var verdicts []inspector.Verdict
	if err := json.Unmarshal([]byte(body), &verdicts); err != nil {
		t.Fatalf("decode verdicts from %q: %v", body, err)
	}
	return verdicts
}

// wave8GetVerdictsSoft GETs the verdicts endpoint without failing the test on a
// transient error — for a waitFor poll while the server comes up.
func wave8GetVerdictsSoft(url string) []inspector.Verdict {
	resp, err := http.Get(url) //nolint:noctx,gosec // test poll against the test's own loopback inspector
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	var verdicts []inspector.Verdict
	if json.NewDecoder(resp.Body).Decode(&verdicts) != nil {
		return nil
	}
	return verdicts
}

// wave8GetBody GETs url and returns the response body as a string.
func wave8GetBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx,gosec // test GET against the test's own loopback inspector
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var b strings.Builder
	for sc.Scan() {
		b.WriteString(sc.Text())
	}
	return b.String()
}
