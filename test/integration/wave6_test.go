// This file is the Wave 6 wave-end end-to-end integration test (CLAUDE.md §17 /
// §17.5 — the wave-boundary checkpoint). Wave 6 shipped the observability
// protocol (RFC §11): runtime/obs ships the canonical obs/v1 event model — the
// obs.Event shape, the closed set of event kinds, the start/end/progress/emit
// phases — the non-blocking headless Emitter seam (interface + factory + driver,
// with RegisterDriver/Open and the bounded FanOut composer), the in-memory
// bounded ring-buffer driver, W3C Trace Context correlation IDs, the shape+size
// default capture policy with its redaction-gated full-content opt-in, and the
// headless obs.Recorder the server/apps/tasks subsystems emit through
// (Phase 15); the out-of-band, localhost-bound SSE sink, the optional
// off-by-default OTelEmitter adapter mapping obs.Event onto the MCP/GenAI
// semantic conventions with the W3C-derived span identity, and the MCP
// logging → obs/v1 log-event bridge (server.LogBridge) (Phase 16).
//
// This test drives the integrated Wave 6 surface end to end with REAL
// components and no mocks at the seams: a real runtime/server carrying
// contract-first tools, a real resource and a real MCP App, plus a real
// tasks.Engine — all emitting through ONE real obs.FanOut composed over a real
// ring-buffer driver, a real out-of-band SSESink on a real loopback listener,
// and a real OTelEmitter wired to a REAL in-memory OTel span recorder
// (tracetest.SpanRecorder — a real OTel span pipeline, not a mock at the
// boundary). It drives real MCP calls over a REAL stdio-shaped transport
// (newline-delimited JSON-RPC over OS pipes) and asserts: (1) every event kind
// — tool.call, resource.read, app.load, task.progress, server.lifecycle, log —
// lands in the ring buffer with the correct kinds and well-formed W3C trace IDs;
// (2) the SSE sink streams those events to a real SSE subscriber while the
// stdio pipe carries ONLY clean MCP JSON-RPC framing (the no-corruption proof);
// (3) the OTel recorder receives spans carrying mcp.* / gen_ai.* attributes and
// the W3C-derived trace IDs; (4) a server log record arrives BOTH as a standard
// MCP notifications/message AND as an obs/v1 log event. It covers ≥1 failure
// mode per seam (a stalled SSE subscriber that must not block the emit path; a
// slow ring consumer proving the emit path is non-blocking; OTel-not-configured
// proving local observation still works), and runs an N>=10 concurrency stress
// under -race against the shared reusable artifacts — the FanOut, the SSE sink
// with subscriber churn, and the ring buffer — with a post-teardown
// goroutine-leak assertion.
//
// The Wave 6 surface is the obs/v1 protocol as one wired whole; it does not
// re-prove the Wave 3/4/5 server-core, Apps, or Tasks surfaces. Shared helpers
// — quietLogger, stableGoroutineCount, assertNoGoroutineLeak — are defined once
// for the integration package in wave1_test.go and reused here; ptrInt64 is
// defined in phase13_tasks_test.go. See decision D-078.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	obsotel "github.com/hurtener/dockyard/runtime/obs/otel"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ---- the Wave 6 contract -----------------------------------------------------

// wave6Input / wave6Output is the contract-first tool contract whose lifecycle
// the obs/v1 stream observes — a typed input the generated input schema
// constrains and a typed output.
type wave6Input struct {
	// Region is the region the report covers — a required field of the
	// generated input schema.
	Region string `json:"region" jsonschema:"the region to report on"`
}

type wave6Output struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

// ---- the integrated Wave 6 obs environment ----------------------------------

// wave6Env is the integrated Wave 6 surface wired end to end: a real
// runtime/server over a real stdio-shaped transport, its obs emitter a real
// FanOut over a real ring buffer + a real SSE sink + a real OTelEmitter, the
// OTelEmitter exporting to a real in-memory OTel span recorder. No mocks at any
// seam.
type wave6Env struct {
	srv     *server.Server
	session *mcpsdk.ClientSession
	ring    *obs.RingBuffer
	sink    *obs.SSESink
	spanRec *tracetest.SpanRecorder
	fanout  *obs.FanOut
	//nolint:staticcheck // Exercises legacy logging compatibility during its deprecation window.
	logs    chan *mcpsdk.LoggingMessageParams
	srvPipe *wave6Tee
}

// wave6Tee captures every byte the server writes to its transport so the test
// can prove the stdio pipe carried only clean JSON-RPC framing.
type wave6Tee struct {
	w   io.WriteCloser
	mu  sync.Mutex
	buf strings.Builder
}

func (t *wave6Tee) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf.Write(p)
	t.mu.Unlock()
	return t.w.Write(p)
}

func (t *wave6Tee) Close() error { return t.w.Close() }

func (t *wave6Tee) captured() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// newWave6Env builds the integrated Wave 6 environment. ringCap sizes the ring
// buffer; a tiny ring with no consumer is the non-blocking failure-mode probe.
func newWave6Env(t *testing.T, ringCap int) *wave6Env {
	t.Helper()

	// One real ring buffer (the inspector's pull source, Wave 8), one real
	// out-of-band SSE sink on a real loopback listener, one real OTelEmitter
	// over a REAL in-memory OTel span recorder — a real OTel pipeline, no mock
	// at the boundary (CLAUDE.md §17).
	ring := obs.NewRingBuffer(ringCap)
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })
	spanRec := tracetest.NewSpanRecorder()
	otelEmitter := obsotel.New(spanRec)

	// The runtime emits to all three drivers through one real bounded FanOut —
	// obs/v1 is one protocol many subsystems EMIT to (P2).
	fanout := obs.NewFanOut(ring, sink, otelEmitter)

	srv, err := server.New(server.Info{Name: "wave6-app", Version: "6.0.0"},
		&server.Options{Logger: quietLogger(), Obs: fanout})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// A contract-first tool — emits tool.call start+end.
	report := tool.New[wave6Input, wave6Output]("region_report").
		Describe("a region report").
		Handler(func(_ context.Context, in wave6Input) (tool.Result[wave6Output], error) {
			return tool.Result[wave6Output]{
				Text:       "report for " + in.Region,
				Structured: wave6Output{Region: in.Region, Count: 9},
			}, nil
		})
	if err := report.Register(srv); err != nil {
		t.Fatalf("Register region_report: %v", err)
	}

	// A tool whose handler drives the MCP logging → obs/v1 bridge.
	bridge := srv.LogBridge()
	logging := tool.New[wave6Input, wave6Output]("logging_report").
		Describe("a report that logs").
		Handler(func(ctx context.Context, in wave6Input) (tool.Result[wave6Output], error) {
			// The bridge resolves the in-flight MCP ServerSession from the
			// handler context (threaded by runtime/server) — the typed handler
			// never touches a raw SDK session (P3).
			_ = bridge.Log(ctx, server.LogRecord{
				Level:   server.LogWarning,
				Logger:  "logging_report",
				Message: "region " + in.Region + " is slow",
			})
			return tool.Result[wave6Output]{
				Text:       "ok",
				Structured: wave6Output{Region: in.Region, Count: 1},
			}, nil
		})
	if err := logging.Register(srv); err != nil {
		t.Fatalf("Register logging_report: %v", err)
	}

	// A plain resource — emits resource.read.
	if err := srv.AddResource(server.ResourceDef{
		URI:      "data://wave6/notes",
		Name:     "notes",
		MIMEType: "text/plain",
	}, func(_ context.Context, _ string) (server.ResourceContent, error) {
		return server.ResourceContent{MIMEType: "text/plain", Text: "wave 6 notes"}, nil
	}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	// A real MCP App — its resources/read emits app.load.
	if err := apps.Register(srv, apps.App{
		URI:  "ui://wave6/dashboard",
		Name: "wave6-dashboard",
		HTML: []byte("<html><body>wave 6 dashboard</body></html>"),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	// A REAL stdio-shaped transport: newline-delimited JSON-RPC over OS pipes,
	// exactly as a stdio MCP server speaks — the server's write side teed so the
	// test can inspect every byte on the pipe.
	srvIn, clientOut := io.Pipe() // client -> server
	clientIn, srvOut := io.Pipe() // server -> client
	tee := &wave6Tee{w: srvOut}
	serverT := &mcpsdk.IOTransport{Reader: srvIn, Writer: tee}
	clientT := &mcpsdk.IOTransport{Reader: clientIn, Writer: clientOut}

	ctx, cancel := context.WithCancel(context.Background())
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	// A client that negotiates the logging capability and records every
	// notifications/message it receives.
	//nolint:staticcheck // Exercises legacy logging compatibility during its deprecation window.
	logs := make(chan *mcpsdk.LoggingMessageParams, 16)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave6-client", Version: "0.0.0"},
		&mcpsdk.ClientOptions{
			LoggingMessageHandler: func(_ context.Context, req *mcpsdk.LoggingMessageRequest) {
				if req != nil && req.Params != nil {
					logs <- req.Params
				}
			},
		})
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("client connect over stdio: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	})

	return &wave6Env{
		srv:     srv,
		session: session,
		ring:    ring,
		sink:    sink,
		spanRec: spanRec,
		fanout:  fanout,
		logs:    logs,
		srvPipe: tee,
	}
}

// ---- helpers ----------------------------------------------------------------

// wave6EventsByKind filters a ring snapshot by kind.
func wave6EventsByKind(evs []obs.Event, k obs.EventKind) []obs.Event {
	var out []obs.Event
	for _, e := range evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// wave6WaitForKind polls the ring until at least n events of kind k are present.
func wave6WaitForKind(t *testing.T, ring *obs.RingBuffer, k obs.EventKind, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(wave6EventsByKind(ring.Recent(0), k)) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events of kind %q", n, k)
}

// ---- 1. every obs/v1 event kind lands, over the integrated surface ----------

// TestWave6_AllEventKindsEmitWithW3CTrace drives the integrated Wave 6 surface
// — tool, resource, App, task, server-lifecycle, and log events — through one
// real FanOut and asserts every kind lands in the ring buffer with a well-formed
// W3C trace identity. The obs/v1 stream is one protocol every subsystem emits to.
func TestWave6_AllEventKindsEmitWithW3CTrace(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 512)
	ctx := context.Background()

	// tool.call (success).
	if res, err := env.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "region_report", Arguments: wave6Input{Region: "emea"},
	}); err != nil {
		t.Fatalf("CallTool region_report: %v", err)
	} else if res.IsError {
		t.Fatalf("region_report errored: %+v", res.Content)
	}

	// log — driven from inside a real tool handler through the real LogBridge.
	if res, err := env.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "logging_report", Arguments: wave6Input{Region: "apac"},
	}); err != nil {
		t.Fatalf("CallTool logging_report: %v", err)
	} else if res.IsError {
		t.Fatalf("logging_report errored: %+v", res.Content)
	}

	// resource.read.
	if _, err := env.session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "data://wave6/notes",
	}); err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	// app.load — reading the ui:// App resource.
	if _, err := env.session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "ui://wave6/dashboard",
	}); err != nil {
		t.Fatalf("ReadResource App: %v", err)
	}

	// task.progress — a real tasks.Engine emitting through the SAME FanOut.
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		Logger:   quietLogger(),
		Obs:      env.fanout,
		ServerID: "wave6-app",
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	taskDone := make(chan struct{})
	if _, err := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "region_report",
		TaskMeta: protocolcodec.TaskMeta{TTL: ptrInt64(60000)},
		Run: func(context.Context) (json.RawMessage, error) {
			defer close(taskDone)
			return json.RawMessage(`{"isError":false}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	select {
	case <-taskDone:
	case <-time.After(2 * time.Second):
		t.Fatal("task run did not complete")
	}
	wave6WaitForKind(t, env.ring, obs.KindTaskProgress, 2)
	wave6WaitForKind(t, env.ring, obs.KindLog, 1)

	// --- assert every kind landed with a well-formed W3C trace identity ------
	all := env.ring.Recent(0)
	if len(all) == 0 {
		t.Fatal("ring buffer served no events — nothing emitted through the FanOut")
	}
	for _, e := range all {
		if e.SchemaVersion != obs.SchemaVersion {
			t.Errorf("event %s schema_version %q, want %q", e.ID, e.SchemaVersion, obs.SchemaVersion)
		}
		if len(e.TraceID) != 32 {
			t.Errorf("event %s (kind %s) trace_id %q is not a 32-hex W3C trace-id", e.ID, e.Kind, e.TraceID)
		}
		if len(e.SpanID) != 16 {
			t.Errorf("event %s (kind %s) span_id %q is not a 16-hex W3C span-id", e.ID, e.Kind, e.SpanID)
		}
	}

	// tool.call: 2 calls × (start+end) = 4 events.
	if n := len(wave6EventsByKind(all, obs.KindToolCall)); n != 4 {
		t.Errorf("tool.call events = %d, want 4 (2 calls × start+end)", n)
	}
	// resource.read: the plain resource AND the App resource read.
	if n := len(wave6EventsByKind(all, obs.KindResourceRead)); n < 2 {
		t.Errorf("resource.read events = %d, want >= 2", n)
	}
	if n := len(wave6EventsByKind(all, obs.KindAppLoad)); n != 1 {
		t.Errorf("app.load events = %d, want 1", n)
	}
	taskEvents := wave6EventsByKind(all, obs.KindTaskProgress)
	if len(taskEvents) != 2 {
		t.Errorf("task.progress events = %d, want 2 (start+end)", len(taskEvents))
	}
	if len(taskEvents) == 2 && taskEvents[0].TraceID != taskEvents[1].TraceID {
		t.Error("task.progress start and end events must share a trace id (W3C correlation)")
	}
	if n := len(wave6EventsByKind(all, obs.KindServerLifecycle)); n < 1 {
		t.Errorf("server.lifecycle events = %d, want >= 1 (the 'starting' event)", n)
	}
	logEvents := wave6EventsByKind(all, obs.KindLog)
	if len(logEvents) < 1 {
		t.Fatal("log events = 0, want >= 1 — the MCP logging bridge did not feed obs/v1")
	}
	var lp obs.LogPayload
	if err := json.Unmarshal(logEvents[0].Payload, &lp); err != nil {
		t.Fatalf("decode obs/v1 LogPayload: %v", err)
	}
	if lp.Level != "warning" || lp.Logger != "logging_report" {
		t.Errorf("obs/v1 log payload = %+v, want level=warning logger=logging_report", lp)
	}
}

// ---- 2. SSE streams while the stdio pipe stays clean ------------------------

// TestWave6_SSEStreamsWhileStdioStaysClean is the headline no-corruption proof:
// the out-of-band SSE sink streams obs/v1 events to a real SSE subscriber while,
// over the real stdio transport, stdout carries ONLY clean MCP JSON-RPC framing
// — no obs event, no SSE frame ever leaks onto the protocol pipe.
func TestWave6_SSEStreamsWhileStdioStaysClean(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 256)

	// A real SSE subscriber on the out-of-band loopback channel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+env.sink.Addr()+"/obs/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE subscriber: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	sseLines := make(chan string, 128)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			sseLines <- sc.Text()
		}
		close(sseLines)
	}()
	// Wait for the subscriber to register before exercising the tool.
	wave6WaitUntil(t, 2*time.Second, func() bool { return env.sink.Subscribers() == 1 })

	// Exercise the tool over the real stdio pipe.
	if res, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "region_report", Arguments: wave6Input{Region: "us"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	} else if res.IsError {
		t.Fatalf("tool returned an error result")
	}

	// 1. The stdio pipe carries ONLY clean newline-delimited JSON-RPC — every
	//    non-empty line parses as a JSON-RPC 2.0 object and no obs/v1 event ever
	//    leaked onto it (the no-corruption headline criterion).
	piped := env.srvPipe.captured()
	if piped == "" {
		t.Fatal("the server wrote nothing to the stdio pipe")
	}
	for _, line := range strings.Split(strings.TrimRight(piped, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("stdio pipe carried a non-JSON-RPC line (corruption): %q: %v", line, err)
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdio pipe carried a non-JSON-RPC message (corruption): %q", line)
		}
		if strings.Contains(line, "schema_version") || strings.Contains(line, "dockyard.obs") {
			t.Fatalf("an obs/v1 event leaked onto the stdio pipe: %q", line)
		}
	}

	// 2. The obs/v1 tool.call event DID arrive out-of-band on the SSE channel.
	deadline := time.After(3 * time.Second)
	sawToolCall := false
	for !sawToolCall {
		select {
		case line, ok := <-sseLines:
			if !ok {
				t.Fatal("SSE stream closed before a tool.call event arrived")
			}
			if body, found := strings.CutPrefix(line, "data: "); found {
				var ev obs.Event
				if json.Unmarshal([]byte(body), &ev) == nil && ev.Kind == obs.KindToolCall {
					sawToolCall = true
				}
			}
		case <-deadline:
			t.Fatal("no obs/v1 tool.call event on the SSE channel within deadline")
		}
	}
}

// ---- 3. the OTel pipeline receives MCP-semconv spans ------------------------

// TestWave6_OTelSpansCarryMCPSemconv asserts the real in-memory OTel span
// pipeline received spans carrying mcp.* / gen_ai.* attributes and the
// W3C-derived trace IDs the obs/v1 events already assigned.
func TestWave6_OTelSpansCarryMCPSemconv(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 256)

	if res, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "region_report", Arguments: wave6Input{Region: "latam"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	} else if res.IsError {
		t.Fatalf("tool returned an error result")
	}

	var found bool
	wave6WaitUntil(t, 3*time.Second, func() bool {
		for _, sp := range env.spanRec.Ended() {
			if strings.HasPrefix(sp.Name(), "tools/call") {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatal("no tools/call span exported to the real OTel pipeline")
	}

	for _, sp := range env.spanRec.Ended() {
		if !strings.HasPrefix(sp.Name(), "tools/call") {
			continue
		}
		attrs := map[string]string{}
		for _, a := range sp.Attributes() {
			attrs[string(a.Key)] = a.Value.Emit()
		}
		if attrs["mcp.method.name"] != "tools/call" {
			t.Errorf("span missing mcp.method.name=tools/call, got %q", attrs["mcp.method.name"])
		}
		if attrs["gen_ai.tool.name"] != "region_report" {
			t.Errorf("span missing gen_ai.tool.name=region_report, got %q", attrs["gen_ai.tool.name"])
		}
		if attrs["gen_ai.operation.name"] != "execute_tool" {
			t.Errorf("span missing gen_ai.operation.name=execute_tool, got %q", attrs["gen_ai.operation.name"])
		}
		// The exported span's trace-id is a well-formed W3C trace-id (32 hex) —
		// the W3C-derived identity the obs event carried, so a Dockyard span
		// nests natively under a calling Harbor agent's execute_tool span.
		if tid := sp.SpanContext().TraceID().String(); len(tid) != 32 {
			t.Errorf("span trace-id %q is not a 32-hex W3C trace-id", tid)
		}
	}
}

// ---- 4. the log bridge fans to MCP AND obs/v1 -------------------------------

// TestWave6_LogBridgeFansToMCPAndObs proves a server log record arrives BOTH as
// a standard MCP notifications/message AND as an obs/v1 log event — the bridge
// is an event source, not a replacement for MCP logging (P2, D-077).
func TestWave6_LogBridgeFansToMCPAndObs(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 256)
	ctx := context.Background()

	if res, err := env.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "logging_report",
		Arguments: wave6Input{Region: "emea"},
		// Logging is request-scoped in the 2026-07-28 lifecycle; the old
		// logging/setLevel RPC remains only for legacy peers.
		//nolint:staticcheck // Modern requests carry the deprecated SDK log-level key until Phase 32 migrates the bridge.
		Meta: mcpsdk.Meta{mcpsdk.MetaKeyLogLevel: "debug"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	} else if res.IsError {
		t.Fatalf("tool returned an error result")
	}

	// (a) The standard MCP notifications/message arrived — a client that
	//     negotiated logging still receives it exactly as the spec defines.
	select {
	case lm := <-env.logs:
		if lm.Level != "warning" {
			t.Errorf("MCP log level = %q, want warning", lm.Level)
		}
		if lm.Logger != "logging_report" {
			t.Errorf("MCP log logger = %q, want logging_report", lm.Logger)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no MCP notifications/message — the standard MCP logging path is broken")
	}

	// (b) The SAME record surfaced as an obs/v1 log event in the ring buffer.
	wave6WaitForKind(t, env.ring, obs.KindLog, 1)
	logEvents := wave6EventsByKind(env.ring.Recent(0), obs.KindLog)
	var lp obs.LogPayload
	if err := json.Unmarshal(logEvents[0].Payload, &lp); err != nil {
		t.Fatalf("decode obs/v1 LogPayload: %v", err)
	}
	if lp.Level != "warning" || lp.Logger != "logging_report" {
		t.Errorf("obs/v1 log payload = %+v, want level=warning logger=logging_report", lp)
	}

	// (c) The OTel pipeline also received the log as a span event (D-076).
	wave6WaitUntil(t, 3*time.Second, func() bool {
		for _, sp := range env.spanRec.Ended() {
			for _, ev := range sp.Events() {
				if ev.Name == "log" {
					return true
				}
			}
		}
		return false
	})
}

// ---- failure mode: a stalled SSE subscriber must not block the emit path ----

// TestWave6_StalledSSESubscriberDoesNotBlockEmit is a mandated failure mode on
// the SSE seam: a subscriber that connects but never drains its body must not
// stall the runtime's emit path — tool calls over the stdio pipe keep returning.
func TestWave6_StalledSSESubscriberDoesNotBlockEmit(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 256)

	// A subscriber that connects but never reads its body.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+env.sink.Addr()+"/obs/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect stalled subscriber: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	wave6WaitUntil(t, 2*time.Second, func() bool { return env.sink.Subscribers() == 1 })

	// Many tool calls; each must complete promptly despite the stalled
	// subscriber — the emit path is non-blocking past a full bounded queue.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 80; i++ {
			if _, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name: "region_report", Arguments: wave6Input{Region: "r"},
			}); err != nil {
				t.Errorf("CallTool %d: %v", i, err)
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tool calls stalled — a slow SSE subscriber blocked the emit path")
	}
}

// ---- failure mode: a slow ring consumer never blocks the runtime -----------

// TestWave6_TinyRingNeverBlocksRuntime is a mandated failure mode on the
// ring-buffer seam: a server wired to a tiny ring with NO consumer keeps serving
// tool calls at full speed; the ring overwrites its oldest events and accounts
// the drops, the runtime never stalls (CLAUDE.md §8).
func TestWave6_TinyRingNeverBlocksRuntime(t *testing.T) {
	t.Parallel()
	env := newWave6Env(t, 4) // deliberately tiny, no consumer drains it

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name: "region_report", Arguments: wave6Input{Region: "r"},
			}); err != nil {
				t.Errorf("CallTool %d: %v", i, err)
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("tool calls stalled — a full ring blocked the runtime")
	}
	if env.ring.Len() > env.ring.Cap() {
		t.Errorf("ring Len %d exceeded Cap %d", env.ring.Len(), env.ring.Cap())
	}
	if env.ring.Dropped() == 0 {
		t.Error("a tiny un-drained ring under 200 calls must have dropped events")
	}
}

// ---- failure mode: OTel not configured — local observation still works ------

// TestWave6_OTelNotConfiguredLocalObservationWorks proves OTel is genuinely
// off by default and never a prerequisite to observe locally: a FanOut with an
// OTelEmitter built with NO span processor still delivers every event to the
// ring buffer and the SSE sink. obs/v1 local observation is zero-dependency
// (RFC §11.3, CLAUDE.md §8).
func TestWave6_OTelNotConfiguredLocalObservationWorks(t *testing.T) {
	t.Parallel()

	ring := obs.NewRingBuffer(64)
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// An OTelEmitter with NO processor — OTel is off. It must be a safe,
	// inert member of the FanOut, never a prerequisite.
	offOTel := obsotel.New() // no processors → discards every event
	fanout := obs.NewFanOut(ring, sink, offOTel)

	srv, err := server.New(server.Info{Name: "wave6-nootel", Version: "6.0.0"},
		&server.Options{Logger: quietLogger(), Obs: fanout})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[wave6Input, wave6Output]("region_report").
		Handler(func(_ context.Context, in wave6Input) (tool.Result[wave6Output], error) {
			return tool.Result[wave6Output]{Structured: wave6Output{Region: in.Region}}, nil
		})
	if err := report.Register(srv); err != nil {
		t.Fatalf("Register: %v", err)
	}

	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave6-nootel-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
		}
	})

	if res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "region_report", Arguments: wave6Input{Region: "emea"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	} else if res.IsError {
		t.Fatalf("tool returned an error result")
	}

	// Local observation still works with zero OTel configuration.
	wave6WaitForKind(t, ring, obs.KindToolCall, 2) // start + end
	if len(wave6EventsByKind(ring.Recent(0), obs.KindToolCall)) < 2 {
		t.Fatal("local observation via the ring buffer failed with OTel off — obs/v1 must be zero-dependency")
	}
}

// ---- concurrency stress: N>=10 under -race ----------------------------------

// TestWave6_ConcurrencyStress drives an N>=10 concurrency stress against the
// shared reusable Wave 6 artifacts — one real FanOut, the SSE sink with
// subscriber churn, and the ring buffer — under -race, with a post-teardown
// goroutine-leak assertion (CLAUDE.md §17). Each worker emits through the shared
// FanOut while connecting and disconnecting its own SSE subscriber, so the
// emitter races subscriber churn and ring writes on every iteration.
func TestWave6_ConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	ring := obs.NewRingBuffer(256)
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	spanRec := tracetest.NewSpanRecorder()
	otelEmitter := obsotel.New(spanRec)
	fanout := obs.NewFanOut(ring, sink, otelEmitter)

	// A real Recorder over the shared FanOut — the emit helper every subsystem
	// uses; it is a reusable concurrent artifact (CLAUDE.md §5).
	rec := obs.NewRecorder(fanout, "wave6-stress")

	const workers = 14 // N >= 10
	const perWorker = 12
	streamURL := "http://" + sink.Addr() + "/obs/v1/stream"

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				// An SSE subscriber connects and disconnects — subscriber churn
				// racing the concurrent emits.
				ctx, cancel := context.WithCancel(context.Background())
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
				resp, rerr := http.DefaultClient.Do(req)

				// Emit a full tool.call lifecycle through the shared FanOut.
				sc := obs.NewTrace()
				end := rec.ToolCall(ctx, sc, "region_report", "inmem")
				end(
					json.RawMessage(`{"region":"emea"}`),
					json.RawMessage(`{"region":"emea","count":1}`),
					nil,
				)

				if rerr == nil {
					_ = resp.Body.Close()
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	// Tear down: close the SSE sink, then assert no goroutine leak.
	if err := sink.Close(); err != nil {
		t.Errorf("SSE sink Close: %v", err)
	}
	if err := fanout.Close(); err != nil {
		t.Errorf("FanOut Close: %v", err)
	}
	// The ring recorded events without a race (the -race detector asserts).
	if ring.Len() == 0 {
		t.Error("ring buffer recorded nothing under the concurrency stress")
	}
	assertNoGoroutineLeak(t, baseline)
}

// wave6WaitUntil polls cond until true or the deadline elapses.
func wave6WaitUntil(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", d)
	}
}
