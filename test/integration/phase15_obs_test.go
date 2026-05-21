// Phase 15 integration test (CLAUDE.md §17). Phase 15's Deps name Phase 07 and
// it instruments runtime/server, runtime/apps and runtime/tasks to emit the
// obs/v1 event stream; it also opens the emitter seam Phase 16's SSE sink and
// OTel adapter plug into. This test drives a REAL runtime/server over the real
// in-memory transport with real contract-first tools, a real resource, a real
// MCP App, and a real tasks.Engine — no mocks at any seam — and asserts the
// corresponding obs/v1 events land in a real ring-buffer emitter with the
// correct kinds, phases, and W3C Trace Context IDs. It covers a failure mode (a
// handler error surfacing as an obs ErrorInfo) and runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// phase15Input / phase15Output is the contract-first tool contract.
type phase15Input struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type phase15Output struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

// connectPhase15 serves srv over the in-memory transport and returns a
// connected client session, cleaned up on test end.
func connectPhase15(t *testing.T, srv *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "phase15-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
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
	return session
}

// eventsByKind filters a ring-buffer snapshot by kind.
func eventsByKind(evs []obs.Event, k obs.EventKind) []obs.Event {
	var out []obs.Event
	for _, e := range evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// assertWellFormedTrace fails the test if any event carries a malformed W3C
// trace/span id, or a wrong schema version.
func assertWellFormedTrace(t *testing.T, evs []obs.Event) {
	t.Helper()
	for _, e := range evs {
		if e.SchemaVersion != obs.SchemaVersion {
			t.Errorf("event %s has schema_version %q, want %q", e.ID, e.SchemaVersion, obs.SchemaVersion)
		}
		if len(e.TraceID) != 32 {
			t.Errorf("event %s trace_id %q is not a 32-hex W3C trace-id", e.ID, e.TraceID)
		}
		if len(e.SpanID) != 16 {
			t.Errorf("event %s span_id %q is not a 16-hex W3C span-id", e.ID, e.SpanID)
		}
	}
}

// TestPhase15_ObsEventsEndToEnd is the binding integration test: tool, resource,
// app, and task events all emit into one real ring-buffer emitter, the emitter
// never blocks, and the ring serves recent events.
func TestPhase15_ObsEventsEndToEnd(t *testing.T) {
	t.Parallel()

	// One real ring-buffer emitter shared by the server and the tasks engine —
	// the obs/v1 stream is one protocol many subsystems EMIT to (P2).
	ring := obs.NewRingBuffer(512)

	srv, err := server.New(server.Info{Name: "phase15-app", Version: "1.0.0"}, &server.Options{
		Logger: quietLogger(),
		Obs:    ring,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// A real contract-first tool — emits tool.call.
	ok := tool.New[phase15Input, phase15Output]("region_report").
		Describe("A region report").
		Handler(func(_ context.Context, in phase15Input) (tool.Result[phase15Output], error) {
			return tool.Result[phase15Output]{
				Structured: phase15Output{Region: in.Region, Count: 7},
			}, nil
		})
	if err := ok.Register(srv); err != nil {
		t.Fatalf("Register region_report: %v", err)
	}

	// A tool whose handler fails — the obs failure-mode coverage.
	bad := tool.New[phase15Input, phase15Output]("failing_report").
		Describe("A failing report").
		Handler(func(_ context.Context, _ phase15Input) (tool.Result[phase15Output], error) {
			return tool.Result[phase15Output]{}, errors.New("report generation failed")
		})
	if err := bad.Register(srv); err != nil {
		t.Fatalf("Register failing_report: %v", err)
	}

	// A plain resource — emits resource.read.
	const resURI = "data://phase15/notes"
	if err := srv.AddResource(server.ResourceDef{
		URI:      resURI,
		Name:     "notes",
		MIMEType: "text/plain",
	}, func(_ context.Context, _ string) (server.ResourceContent, error) {
		return server.ResourceContent{MIMEType: "text/plain", Text: "phase 15 notes"}, nil
	}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	// A real MCP App — its resources/read emits app.load.
	const appURI = "ui://phase15/dashboard"
	const appHTML = "<html><body>phase 15 dashboard</body></html>"
	if err := apps.Register(srv, apps.App{
		URI:  appURI,
		Name: "phase15-dashboard",
		HTML: []byte(appHTML),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	session := connectPhase15(t, srv)
	ctx := context.Background()

	// --- drive tool.call (success) -------------------------------------------
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "region_report",
		Arguments: phase15Input{Region: "emea"},
	})
	if err != nil {
		t.Fatalf("CallTool region_report: %v", err)
	}
	if res.IsError {
		t.Fatalf("region_report unexpectedly errored: %+v", res.Content)
	}

	// --- drive tool.call (failure — the obs failure mode) --------------------
	failRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "failing_report",
		Arguments: phase15Input{Region: "emea"},
	})
	if err != nil {
		t.Fatalf("CallTool failing_report transport error: %v", err)
	}
	if !failRes.IsError {
		t.Fatal("failing_report should have produced a tool error result")
	}

	// --- drive resource.read -------------------------------------------------
	if _, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: resURI}); err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	// --- drive app.load ------------------------------------------------------
	if _, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: appURI}); err != nil {
		t.Fatalf("ReadResource App: %v", err)
	}

	// --- drive task events with a real tasks.Engine --------------------------
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		Logger:   quietLogger(),
		Obs:      ring, // same ring — one obs/v1 stream
		ServerID: "phase15-app",
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
			return json.RawMessage(`{"ok":true}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	select {
	case <-taskDone:
	case <-time.After(2 * time.Second):
		t.Fatal("task run did not complete")
	}
	// The terminal task event is emitted from the run goroutine after the
	// handler returns; give it a brief moment to land.
	waitForKind(t, ring, obs.KindTaskProgress, 2)

	// --- assert every kind landed --------------------------------------------
	all := ring.Recent(0)
	if len(all) == 0 {
		t.Fatal("ring buffer served no events — nothing emitted")
	}
	assertWellFormedTrace(t, all)

	toolEvents := eventsByKind(all, obs.KindToolCall)
	// 2 calls × (start+end) = 4 tool.call events.
	if len(toolEvents) != 4 {
		t.Errorf("tool.call events = %d, want 4 (2 calls × start+end)", len(toolEvents))
	}
	assertStartEndPaired(t, toolEvents, "tool.call")

	// The failing tool's end event must carry an obs ErrorInfo (the failure
	// mode — a protocol-masked failure made visible, brief 05 §2.2).
	var sawToolError bool
	for _, e := range toolEvents {
		if e.Phase == obs.PhaseEnd && e.Error != nil {
			sawToolError = true
			if e.Error.Message == "" {
				t.Error("tool.call error event has an empty message")
			}
		}
	}
	if !sawToolError {
		t.Error("the failing tool must produce a tool.call end event with ErrorInfo")
	}

	if n := len(eventsByKind(all, obs.KindResourceRead)); n < 2 {
		// resource.read fires for the plain resource AND the App resource read.
		t.Errorf("resource.read events = %d, want >= 2", n)
	}
	if n := len(eventsByKind(all, obs.KindAppLoad)); n != 1 {
		t.Errorf("app.load events = %d, want 1", n)
	}
	taskEvents := eventsByKind(all, obs.KindTaskProgress)
	if len(taskEvents) != 2 {
		t.Errorf("task.progress events = %d, want 2 (start+end)", len(taskEvents))
	}
	// The task's start and end events share a trace — W3C correlation.
	if len(taskEvents) == 2 && taskEvents[0].TraceID != taskEvents[1].TraceID {
		t.Error("task.progress start and end events must share a trace id")
	}
	if n := len(eventsByKind(all, obs.KindServerLifecycle)); n < 1 {
		t.Errorf("server.lifecycle events = %d, want >= 1 (the 'starting' event)", n)
	}
}

// assertStartEndPaired checks every start event has a matching end event on the
// same span — the obs/v1 lifecycle-pairing invariant.
func assertStartEndPaired(t *testing.T, evs []obs.Event, kind string) {
	t.Helper()
	bySpan := map[string][]obs.Phase{}
	for _, e := range evs {
		bySpan[e.SpanID] = append(bySpan[e.SpanID], e.Phase)
	}
	for span, phases := range bySpan {
		if len(phases) != 2 {
			t.Errorf("%s span %s has %d events, want a start+end pair", kind, span, len(phases))
			continue
		}
		var hasStart, hasEnd bool
		for _, p := range phases {
			if p == obs.PhaseStart {
				hasStart = true
			}
			if p == obs.PhaseEnd {
				hasEnd = true
			}
		}
		if !hasStart || !hasEnd {
			t.Errorf("%s span %s is not a start+end pair: %v", kind, span, phases)
		}
	}
}

// waitForKind polls the ring buffer until at least n events of kind k are
// present, or fails after a short timeout — the terminal task event is emitted
// asynchronously from the run goroutine.
func waitForKind(t *testing.T, ring *obs.RingBuffer, k obs.EventKind, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(eventsByKind(ring.Recent(0), k)) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events of kind %q", n, k)
}

// TestPhase15_EmitterNeverBlocksRuntime proves the binding acceptance criterion
// "the emitter never blocks on a slow consumer" at the integration layer: a
// server wired to a tiny ring buffer with NO consumer keeps serving tool calls
// at full speed. The ring overwrites its oldest events; the runtime never
// stalls.
func TestPhase15_EmitterNeverBlocksRuntime(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(4) // deliberately tiny, no consumer drains it

	srv, err := server.New(server.Info{Name: "phase15-fast", Version: "1.0.0"}, &server.Options{
		Logger: quietLogger(),
		Obs:    ring,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	fast := tool.New[phase15Input, phase15Output]("fast").
		Handler(func(_ context.Context, in phase15Input) (tool.Result[phase15Output], error) {
			return tool.Result[phase15Output]{Structured: phase15Output{Region: in.Region}}, nil
		})
	if err := fast.Register(srv); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connectPhase15(t, srv)
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "fast",
				Arguments: phase15Input{Region: "emea"},
			}); err != nil {
				t.Errorf("CallTool %d: %v", i, err)
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
		// Every call returned — the un-drained ring never blocked the runtime.
	case <-time.After(15 * time.Second):
		t.Fatal("tool calls stalled — the emitter blocked the runtime on a full ring")
	}
	// The ring stayed bounded and accounted its drops.
	if ring.Len() > ring.Cap() {
		t.Errorf("ring Len %d exceeded Cap %d", ring.Len(), ring.Cap())
	}
	if ring.Dropped() == 0 {
		t.Error("a tiny un-drained ring under 200 calls must have dropped events")
	}
}
