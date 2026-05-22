package otel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hurtener/dockyard/runtime/obs"
)

// recordingEmitter builds an OTelEmitter over an in-memory span recorder — a
// REAL OTel span pipeline, no mock at the boundary (CLAUDE.md §17).
func recordingEmitter(t *testing.T) (*OTelEmitter, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	return New(rec), rec
}

// event builds a valid obs/v1 event with a typed payload.
func event(t *testing.T, kind obs.EventKind, phase obs.Phase, payload any) obs.Event {
	t.Helper()
	sc := obs.NewTrace()
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		raw = b
	}
	return obs.Event{
		SchemaVersion: obs.SchemaVersion,
		ID:            "0123456789abcdef0123456789abcdef",
		Timestamp:     time.Now().UTC(),
		ServerID:      "test-server",
		SessionID:     "sess-7",
		TraceID:       sc.TraceID,
		SpanID:        sc.SpanID,
		Kind:          kind,
		Phase:         phase,
		Payload:       raw,
	}
}

// attrMap collapses a span's attributes into a lookup map.
func attrMap(attrs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.Emit()
	}
	return m
}

func TestOTelEmitter_OffByDefault(t *testing.T) {
	t.Parallel()
	// A nil provider must yield an emitter that exports nothing — OTel is off
	// unless a real provider is supplied (CLAUDE.md §8).
	e := New(nil)
	e.Emit(context.Background(), event(t, obs.KindToolCall, obs.PhaseEnd,
		obs.ToolCallPayload{Tool: "search"}))
	// No panic, no provider, nothing to assert beyond "does not crash".

	// A nil *OTelEmitter is also safe.
	var nilE *OTelEmitter
	nilE.Emit(context.Background(), event(t, obs.KindToolCall, obs.PhaseEnd, nil))
}

func TestOTelEmitter_DriverRegisteredAndInert(t *testing.T) {
	t.Parallel()
	found := false
	for _, d := range obs.Drivers() {
		if d == driverName {
			found = true
		}
	}
	if !found {
		t.Fatalf("driver %q not registered behind the obs emitter seam", driverName)
	}
	// Opening the driver by name must yield the off-by-default NopEmitter — the
	// seam's string config cannot carry a live TracerProvider.
	e, err := obs.Open(driverName, "")
	if err != nil {
		t.Fatalf("Open(%q): %v", driverName, err)
	}
	if _, ok := e.(obs.NopEmitter); !ok {
		t.Fatalf("Open(%q) = %T, want obs.NopEmitter (off by default)", driverName, e)
	}
}

func TestOTelEmitter_ToolCallSpanCarriesMCPSemconv(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	contractOK := true
	ev := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{
		Tool:       "search",
		Transport:  "stdio",
		ContractOK: &contractOK,
	})
	dur := int64(42)
	ev.DurationMS = &dur
	e.Emit(context.Background(), ev)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	sp := spans[0]
	if sp.Name() != "tools/call search" {
		t.Fatalf("span name = %q, want %q", sp.Name(), "tools/call search")
	}
	m := attrMap(sp.Attributes())
	// The headline acceptance criterion: mcp.* / gen_ai.* attributes present.
	wantAttrs := map[string]string{
		attrMCPMethodName:      "tools/call",
		attrGenAIToolName:      "search",
		attrGenAIOperationName: genAIOperationExecuteTool,
		attrMCPSessionID:       "sess-7",
		attrNetworkTransport:   "stdio",
	}
	for k, want := range wantAttrs {
		if got := m[k]; got != want {
			t.Errorf("attribute %q = %q, want %q", k, got, want)
		}
	}
	if m[attrDockyardContractOK] != "true" {
		t.Errorf("attribute %q = %q, want true", attrDockyardContractOK, m[attrDockyardContractOK])
	}
	// W3C-derived trace ID: the span shares the obs event's trace identity.
	if got := sp.SpanContext().TraceID().String(); got != ev.TraceID {
		t.Errorf("span trace-id = %q, want obs event trace-id %q", got, ev.TraceID)
	}
	if got := sp.SpanContext().SpanID().String(); got != ev.SpanID {
		t.Errorf("span span-id = %q, want obs event span-id %q", got, ev.SpanID)
	}
}

func TestOTelEmitter_ResourceReadSpanCarriesResourceURI(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	e.Emit(context.Background(), event(t, obs.KindResourceRead, obs.PhaseEnd,
		obs.ResourceReadPayload{URI: "ui://card", MIME: "text/html", Bytes: 1200}))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	m := attrMap(spans[0].Attributes())
	if m[attrMCPMethodName] != "resources/read" {
		t.Errorf("%q = %q, want resources/read", attrMCPMethodName, m[attrMCPMethodName])
	}
	if m[attrMCPResourceURI] != "ui://card" {
		t.Errorf("%q = %q, want ui://card", attrMCPResourceURI, m[attrMCPResourceURI])
	}
}

func TestOTelEmitter_ErrorEventCarriesErrorType(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	ev := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{Tool: "search"})
	ev.Error = &obs.ErrorInfo{Type: "handler_error", Message: "boom"}
	e.Emit(context.Background(), ev)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	m := attrMap(spans[0].Attributes())
	if m[attrErrorType] != "handler_error" {
		t.Errorf("%q = %q, want handler_error", attrErrorType, m[attrErrorType])
	}
}

func TestOTelEmitter_StartEventDoesNotExport(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)
	// A start event is the open half of a pair — exporting on the end event
	// keeps one OTel span per obs unit of work.
	e.Emit(context.Background(), event(t, obs.KindToolCall, obs.PhaseStart,
		obs.ToolCallPayload{Tool: "search"}))
	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("start event produced %d spans, want 0", got)
	}
}

func TestOTelEmitter_LogEventExportsSpanEvent(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	e.Emit(context.Background(), event(t, obs.KindLog, obs.PhaseEmit,
		obs.LogPayload{Level: "warning", Logger: "tool", Message: "slow query"}))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 || events[0].Name != "log" {
		t.Fatalf("span events = %+v, want one 'log' event", events)
	}
	m := attrMap(events[0].Attributes)
	if m["log.level"] != "warning" || m["log.message"] != "slow query" {
		t.Errorf("log span-event attributes = %v, want level=warning message=\"slow query\"", m)
	}
}

func TestOTelEmitter_InvalidIDsDropped(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)
	ev := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{Tool: "x"})
	ev.TraceID = "not-hex"
	e.Emit(context.Background(), ev)
	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("event with invalid trace-id produced %d spans, want 0", got)
	}
}

// TestOTelEmitter_ParentSpanIDNests proves the exported span nests under the
// obs/v1 event's ParentSpanID (Finding D / D-114): a handler `log` event that
// is a true child of its `tool.call` (the obs/v1 trace-correlation, D-079) must
// keep that parent linkage on OTel export. Before the fix every exported span
// was a trace root.
func TestOTelEmitter_ParentSpanIDNests(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	parentSpanID := "00f067aa0ba902b7" // a well-formed 16-hex W3C span-id.
	ev := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{Tool: "search"})
	ev.ParentSpanID = parentSpanID
	e.Emit(context.Background(), ev)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	sp := spans[0]
	parent := sp.Parent()
	if !parent.IsValid() {
		t.Fatalf("exported span has no parent — it is a trace root; want it nested "+
			"under ParentSpanID %q", parentSpanID)
	}
	if got := parent.SpanID().String(); got != parentSpanID {
		t.Errorf("span parent span-id = %q, want %q", got, parentSpanID)
	}
	// The parent must share the event's trace — a child span, not a cross-trace link.
	if got := parent.TraceID().String(); got != ev.TraceID {
		t.Errorf("parent trace-id = %q, want the event's trace-id %q", got, ev.TraceID)
	}
	// The span itself still carries the event's own span-id.
	if got := sp.SpanContext().SpanID().String(); got != ev.SpanID {
		t.Errorf("span span-id = %q, want event span-id %q", got, ev.SpanID)
	}
}

// TestOTelEmitter_NoParentSpanIDIsRoot proves an event with no ParentSpanID
// still exports as a trace root, and a malformed ParentSpanID is tolerated as
// "no parent" rather than dropping the event.
func TestOTelEmitter_NoParentSpanIDIsRoot(t *testing.T) {
	t.Parallel()
	e, rec := recordingEmitter(t)

	noParent := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{Tool: "a"})
	e.Emit(context.Background(), noParent)

	badParent := event(t, obs.KindToolCall, obs.PhaseEnd, obs.ToolCallPayload{Tool: "b"})
	badParent.ParentSpanID = "not-hex"
	e.Emit(context.Background(), badParent)

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 (a malformed ParentSpanID must not drop the event)", len(spans))
	}
	for _, sp := range spans {
		if sp.Parent().IsValid() {
			t.Errorf("span %q has a parent; an event with no/invalid ParentSpanID must be a root",
				sp.Name())
		}
	}
}
