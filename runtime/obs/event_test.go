package obs

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// fixedEvent builds a fully-populated Event with deterministic IDs/timestamp so
// its serialized shape is stable for golden comparison.
func fixedEvent(t *testing.T) Event {
	t.Helper()
	dur := int64(42)
	return Event{
		SchemaVersion: SchemaVersion,
		ID:            "00112233445566778899aabbccddeeff",
		Timestamp:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
		ServerID:      "test-server",
		SessionID:     "sess-1",
		TraceID:       "0123456789abcdef0123456789abcdef",
		SpanID:        "0123456789abcdef",
		ParentSpanID:  "fedcba9876543210",
		Kind:          KindToolCall,
		Phase:         PhaseEnd,
		Payload:       json.RawMessage(`{"tool":"search"}`),
		DurationMS:    &dur,
		Error:         &ErrorInfo{Type: "handler_error", Message: "boom", Silent: true},
	}
}

// TestEvent_GoldenShape pins the obs/v1 Event JSON wire shape. obs/v1 is a
// public, versioned, third-party-consumable contract (RFC §11.3, CLAUDE.md §8):
// an accidental change to a field name, order, or omitempty behaviour MUST fail
// CI here. A deliberate change bumps SchemaVersion and updates this golden.
func TestEvent_GoldenShape(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(fixedEvent(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const golden = `{"schema_version":"dockyard.obs/v1",` +
		`"id":"00112233445566778899aabbccddeeff",` +
		`"timestamp":"2026-05-21T12:00:00Z",` +
		`"server_id":"test-server",` +
		`"session_id":"sess-1",` +
		`"trace_id":"0123456789abcdef0123456789abcdef",` +
		`"span_id":"0123456789abcdef",` +
		`"parent_span_id":"fedcba9876543210",` +
		`"kind":"tool.call","phase":"end",` +
		`"payload":{"tool":"search"},` +
		`"duration_ms":42,` +
		`"error":{"type":"handler_error","message":"boom","silent":true}}`
	if string(got) != golden {
		t.Errorf("obs/v1 Event wire shape changed — this is a versioned contract.\n got: %s\nwant: %s", got, golden)
	}
}

// TestEvent_GoldenShape_Minimal pins the omitempty behaviour: optional fields
// must drop out of the wire form when unset.
func TestEvent_GoldenShape_Minimal(t *testing.T) {
	t.Parallel()
	e := Event{
		SchemaVersion: SchemaVersion,
		ID:            "abcd",
		Timestamp:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
		ServerID:      "s",
		TraceID:       "t",
		SpanID:        "sp",
		Kind:          KindServerLifecycle,
		Phase:         PhaseEmit,
	}
	got, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const golden = `{"schema_version":"dockyard.obs/v1","id":"abcd",` +
		`"timestamp":"2026-05-21T12:00:00Z","server_id":"s",` +
		`"trace_id":"t","span_id":"sp",` +
		`"kind":"server.lifecycle","phase":"emit"}`
	if string(got) != golden {
		t.Errorf("obs/v1 minimal Event shape changed.\n got: %s\nwant: %s", got, golden)
	}
}

// TestEvent_RoundTrip proves an Event survives a marshal/unmarshal round trip.
func TestEvent_RoundTrip(t *testing.T) {
	t.Parallel()
	want := fixedEvent(t)
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b2, _ := json.Marshal(got)
	if !bytes.Equal(b, b2) {
		t.Errorf("round trip changed the event:\n first: %s\nsecond: %s", b, b2)
	}
}

func TestEventKind_Valid(t *testing.T) {
	t.Parallel()
	valid := []EventKind{
		KindToolCall, KindResourceRead, KindPromptGet, KindAppLoad,
		KindAppBridge, KindUserAction, KindHostCompat, KindLog,
		KindServerLifecycle, KindTaskProgress,
	}
	for _, k := range valid {
		if !k.valid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	if EventKind("nonsense").valid() {
		t.Error("unknown kind should be invalid")
	}
}

func TestPhase_Valid(t *testing.T) {
	t.Parallel()
	for _, p := range []Phase{PhaseStart, PhaseEnd, PhaseProgress, PhaseEmit} {
		if !p.valid() {
			t.Errorf("phase %q should be valid", p)
		}
	}
	if Phase("nonsense").valid() {
		t.Error("unknown phase should be invalid")
	}
}

func TestEvent_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		mut   func(*Event)
		valid bool
	}{
		{"well-formed", func(*Event) {}, true},
		{"wrong schema version", func(e *Event) { e.SchemaVersion = "other" }, false},
		{"empty id", func(e *Event) { e.ID = "" }, false},
		{"empty server id", func(e *Event) { e.ServerID = "" }, false},
		{"malformed trace id", func(e *Event) { e.TraceID = "short" }, false},
		{"malformed span id", func(e *Event) { e.SpanID = "short" }, false},
		{"malformed parent span id", func(e *Event) { e.ParentSpanID = "xyz" }, false},
		{"unknown kind", func(e *Event) { e.Kind = "bogus" }, false},
		{"unknown phase", func(e *Event) { e.Phase = "bogus" }, false},
		{"empty parent span id ok", func(e *Event) { e.ParentSpanID = "" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := fixedEvent(t)
			tc.mut(&e)
			if got := e.valid(); got != tc.valid {
				t.Errorf("valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}
