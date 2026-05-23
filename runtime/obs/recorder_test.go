package obs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// collector is an Emitter that retains every event for assertions. It is
// concurrency-safe so it can back the concurrent-recorder test.
type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) Emit(_ context.Context, e Event) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *collector) all() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

func (c *collector) byKind(k EventKind) []Event {
	var out []Event
	for _, e := range c.all() {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// fixedClock returns a deterministic clock so duration assertions are stable.
func fixedClock(times ...time.Time) func() time.Time {
	var i int
	var mu sync.Mutex
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := times[i]
		if i < len(times)-1 {
			i++
		}
		return t
	}
}

func TestRecorder_NilIsSafe(t *testing.T) {
	t.Parallel()
	var r *Recorder
	// Every method on a nil *Recorder must be a safe no-op.
	end := r.ToolCall(context.Background(), NewTrace(), "t", "stdio")
	end(nil, nil, nil)
	endR := r.ResourceRead(context.Background(), NewTrace(), "uri")
	endR("mime", 0, nil)
	r.AppLoad(context.Background(), NewTrace(), AppLoadPayload{})
	r.TaskEvent(context.Background(), NewTrace(), PhaseStart, TaskProgressPayload{}, nil)
	r.ServerLifecycle(context.Background(), NewTrace(), ServerLifecyclePayload{})
}

func TestRecorder_NilEmitterPromotedToNop(t *testing.T) {
	t.Parallel()
	r := NewRecorder(nil, "srv")
	// Must not panic — a nil emitter is promoted to NopEmitter.
	r.ServerLifecycle(context.Background(), NewTrace(), ServerLifecyclePayload{State: "starting"})
}

func TestRecorder_ToolCall_EmitsStartAndEnd(t *testing.T) {
	t.Parallel()
	c := &collector{}
	t0 := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(150 * time.Millisecond)
	r := NewRecorder(c, "srv", withClock(fixedClock(t0, t1)))

	sc := NewTrace()
	end := r.ToolCall(context.Background(), sc, "search", "stdio")
	end(json.RawMessage(`{"q":"hi"}`), json.RawMessage(`{"n":3}`), nil)

	evs := c.byKind(KindToolCall)
	if len(evs) != 2 {
		t.Fatalf("want 2 tool.call events (start+end), got %d", len(evs))
	}
	start, fin := evs[0], evs[1]
	if start.Phase != PhaseStart || fin.Phase != PhaseEnd {
		t.Fatalf("phases = %q,%q want start,end", start.Phase, fin.Phase)
	}
	// Both events share the span — correlation.
	if start.TraceID != fin.TraceID || start.SpanID != fin.SpanID {
		t.Error("start and end must share the same trace/span for correlation")
	}
	if start.TraceID != sc.TraceID {
		t.Error("event must carry the supplied trace id")
	}
	if start.ServerID != "srv" {
		t.Errorf("ServerID = %q, want srv", start.ServerID)
	}
	if start.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q", start.SchemaVersion)
	}
	// The end event carries a duration.
	if fin.DurationMS == nil || *fin.DurationMS != 150 {
		t.Errorf("DurationMS = %v, want 150", fin.DurationMS)
	}
	// Default capture: shape present, full content absent (CLAUDE.md §7).
	var p ToolCallPayload
	if err := json.Unmarshal(fin.Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if p.InputShape == nil || p.OutputShape == nil {
		t.Error("end event must carry input/output shapes")
	}
	if p.Input != nil || p.Output != nil {
		t.Error("default policy must NOT capture full input/output content")
	}
	if p.Tool != "search" {
		t.Errorf("Tool = %q, want search", p.Tool)
	}
}

func TestRecorder_ToolCall_FullCaptureWithRedactor(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv",
		WithCapturePolicy(CapturePolicyFull), WithRedactor(fakeRedactor{}))
	end := r.ToolCall(context.Background(), NewTrace(), "t", "")
	end(json.RawMessage(`{"secret":"x"}`), json.RawMessage(`{"secret":"y"}`), nil)

	fin := c.byKind(KindToolCall)[1]
	var p ToolCallPayload
	if err := json.Unmarshal(fin.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if string(p.Input) != `{"redacted":true}` || string(p.Output) != `{"redacted":true}` {
		t.Errorf("full capture must route through the redactor, got in=%s out=%s", p.Input, p.Output)
	}
}

func TestRecorder_ToolCall_ErrorInfo(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	end := r.ToolCall(context.Background(), NewTrace(), "t", "")
	end(nil, nil, errors.New("handler blew up"))

	fin := c.byKind(KindToolCall)[1]
	if fin.Error == nil {
		t.Fatal("end event on a failed call must carry ErrorInfo")
	}
	if fin.Error.Message != "handler blew up" {
		t.Errorf("Error.Message = %q", fin.Error.Message)
	}
}

func TestRecorder_ResourceRead(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	end := r.ResourceRead(context.Background(), NewTrace(), "ui://app")
	end("text/html", 2048, nil)

	evs := c.byKind(KindResourceRead)
	if len(evs) != 2 {
		t.Fatalf("want 2 resource.read events, got %d", len(evs))
	}
	var p ResourceReadPayload
	if err := json.Unmarshal(evs[1].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.URI != "ui://app" || p.MIME != "text/html" || p.Bytes != 2048 {
		t.Errorf("payload = %+v", p)
	}
}

func TestRecorder_AppEvents(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	r.AppLoad(context.Background(), NewTrace(), AppLoadPayload{ResourceURI: "ui://x", Bytes: 10})
	r.AppBridge(context.Background(), NewTrace(), AppBridgePayload{ResourceURI: "ui://x", BridgeReady: true})

	if len(c.byKind(KindAppLoad)) != 1 {
		t.Error("want 1 app.load event")
	}
	if len(c.byKind(KindAppBridge)) != 1 {
		t.Error("want 1 app.bridge event")
	}
}

func TestRecorder_TaskEvent(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	sc := NewTrace()
	r.TaskEvent(context.Background(), sc, PhaseStart, TaskProgressPayload{TaskID: "t1", Status: "working"}, nil)
	r.TaskEvent(context.Background(), sc.Child(), PhaseEnd, TaskProgressPayload{TaskID: "t1", Status: "failed"}, errors.New("nope"))

	evs := c.byKind(KindTaskProgress)
	if len(evs) != 2 {
		t.Fatalf("want 2 task.progress events, got %d", len(evs))
	}
	if evs[1].Error == nil {
		t.Error("a failed task event must carry ErrorInfo")
	}
	if evs[0].TraceID != evs[1].TraceID {
		t.Error("task events must share a trace id for correlation")
	}
}

func TestRecorder_ServerLifecycle(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	r.ServerLifecycle(context.Background(), NewTrace(), ServerLifecyclePayload{State: "starting", Tools: 3})
	evs := c.byKind(KindServerLifecycle)
	if len(evs) != 1 || evs[0].Phase != PhaseEmit {
		t.Fatalf("want 1 emit-phase server.lifecycle event, got %v", evs)
	}
}

func TestRecorder_EventsAreWellFormed(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	end := r.ToolCall(context.Background(), NewTrace(), "t", "stdio")
	end(json.RawMessage(`{}`), json.RawMessage(`{}`), nil)
	for _, e := range c.all() {
		if !e.valid() {
			t.Errorf("recorder emitted a structurally invalid event: %+v", e)
		}
		if e.ID == "" {
			t.Error("recorder must stamp a fresh event ID")
		}
	}
}

// TestRecorder_ConcurrentEmit proves a Recorder is a reusable concurrent
// artifact (CLAUDE.md §5): many goroutines record through one Recorder under
// -race, and every event lands.
func TestRecorder_ConcurrentEmit(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")
	const goroutines, perG = 24, 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				end := r.ToolCall(context.Background(), NewTrace(), "t", "stdio")
				end(json.RawMessage(`{"i":1}`), json.RawMessage(`{"o":2}`), nil)
			}
		}()
	}
	wg.Wait()
	// Each iteration emits a start + end = 2 events.
	want := goroutines * perG * 2
	if got := len(c.all()); got != want {
		t.Errorf("got %d events, want %d — a concurrent emit was lost", got, want)
	}
}

func TestWithSession(t *testing.T) {
	t.Parallel()
	ctx := WithSession(context.Background(), "sess-9")
	if got := sessionFromContext(ctx); got != "sess-9" {
		t.Errorf("sessionFromContext = %q, want sess-9", got)
	}
	// WithSession of an empty id leaves ctx unchanged.
	if sessionFromContext(WithSession(context.Background(), "")) != "" {
		t.Error("empty session id must not be stored")
	}
}

func TestWithSpan(t *testing.T) {
	t.Parallel()
	span := NewTrace()
	ctx := WithSpan(context.Background(), span)
	got, ok := SpanFromContext(ctx)
	if !ok {
		t.Fatal("SpanFromContext: ok=false after WithSpan")
	}
	if got != span {
		t.Errorf("SpanFromContext = %+v, want %+v", got, span)
	}
	// A zero-value span leaves ctx unchanged.
	if _, ok := SpanFromContext(WithSpan(context.Background(), SpanContext{})); ok {
		t.Error("a zero-value span must not be stored")
	}
}

// TestRecorder_EmitStampsSessionID proves the R5/S2 fix: when a caller threads
// an MCP session id onto ctx via [WithSession], every event Recorder.emit
// produces from inside that ctx carries Event.SessionID equal to it (D-120).
// Without the fix the public obs/v1 SessionID wire field was always "".
func TestRecorder_EmitStampsSessionID(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")

	ctx := WithSession(context.Background(), "sess-abc123")
	end := r.ToolCall(ctx, NewTrace(), "search", "http")
	end(json.RawMessage(`{}`), json.RawMessage(`{}`), nil)
	r.ResourceRead(ctx, NewTrace(), "ui://app")("text/html", 10, nil)
	r.AppLoad(ctx, NewTrace(), AppLoadPayload{ResourceURI: "ui://app"})
	r.Log(ctx, NewTrace(), LogPayload{Level: "info", Message: "hi"})

	got := c.all()
	if len(got) == 0 {
		t.Fatal("no events emitted")
	}
	for _, e := range got {
		if e.SessionID != "sess-abc123" {
			t.Errorf("event %s/%s SessionID = %q, want sess-abc123 (R5/S2 regression)",
				e.Kind, e.Phase, e.SessionID)
		}
	}
}

// TestRecorder_EmitWithoutSessionIDStaysEmpty proves the other half: an event
// emitted from a ctx that never had WithSession applied carries SessionID ""
// — the wire field is correctly omitted.
func TestRecorder_EmitWithoutSessionIDStaysEmpty(t *testing.T) {
	t.Parallel()
	c := &collector{}
	r := NewRecorder(c, "srv")

	r.ServerLifecycle(context.Background(), NewTrace(), ServerLifecyclePayload{State: "starting"})
	for _, e := range c.all() {
		if e.SessionID != "" {
			t.Errorf("event %s SessionID = %q, want empty (no session on ctx)", e.Kind, e.SessionID)
		}
	}
}

func TestChildOrNewTrace(t *testing.T) {
	t.Parallel()
	// With an enclosing span: a child — same trace id, parent set.
	parent := NewTrace()
	child := ChildOrNewTrace(WithSpan(context.Background(), parent))
	if child.TraceID != parent.TraceID {
		t.Errorf("child trace id = %q, want %q (enclosing span's)", child.TraceID, parent.TraceID)
	}
	if child.ParentID != parent.SpanID {
		t.Errorf("child parent id = %q, want %q (enclosing span id)", child.ParentID, parent.SpanID)
	}
	if child.SpanID == parent.SpanID {
		t.Error("child span id must differ from the parent's")
	}
	// Without an enclosing span: a fresh root trace.
	root := ChildOrNewTrace(context.Background())
	if root.TraceID == "" || root.SpanID == "" {
		t.Error("ChildOrNewTrace with no enclosing span must mint a fresh trace")
	}
	if root.ParentID != "" {
		t.Errorf("a fresh root trace must have no parent, got %q", root.ParentID)
	}
}
