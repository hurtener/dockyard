package obs

import (
	"crypto/rand"
	"encoding/hex"
)

// W3C Trace Context (https://www.w3.org/TR/trace-context/) ID widths. obs/v1
// adopts W3C Trace Context so a Dockyard server's spans nest natively under a
// calling Harbor agent's execute_tool span, and so Phase 16's OTel adapter has
// real, spec-shaped IDs to export (RFC §11.2, brief 05 Q-4).
const (
	traceIDBytes = 16 // 128-bit trace-id → 32 lowercase hex chars
	spanIDBytes  = 8  // 64-bit span-id  → 16 lowercase hex chars
	eventIDBytes = 16 // 128-bit obs event id → 32 lowercase hex chars
)

// SpanContext is a W3C Trace Context span identity. It is the correlation
// handle a subsystem threads through a unit of work: a start event and its
// paired end event share the same SpanContext, and a child unit of work derives
// a SpanContext whose ParentID is the enclosing span.
type SpanContext struct {
	// TraceID is the 16-byte W3C trace-id as 32 lowercase hex characters. It is
	// constant for a whole call chain.
	TraceID string
	// SpanID is the 8-byte W3C span-id as 16 lowercase hex characters,
	// identifying this unit of work.
	SpanID string
	// ParentID is the SpanID of the enclosing span, or "" at the root.
	ParentID string
}

// NewTrace begins a new W3C trace: a fresh trace-id and a fresh root span-id,
// with no parent. Use it at the entry edge of a call chain that did not arrive
// with an inbound W3C traceparent.
func NewTrace() SpanContext {
	return SpanContext{
		TraceID: randHex(traceIDBytes),
		SpanID:  randHex(spanIDBytes),
	}
}

// Child derives a child span within the same trace: the trace-id is preserved,
// a fresh span-id is generated, and the parent's span-id becomes the child's
// ParentID. A zero-value receiver (no trace yet) is promoted to a fresh root
// trace so a caller need not special-case the entry edge.
func (sc SpanContext) Child() SpanContext {
	if sc.TraceID == "" || sc.SpanID == "" {
		return NewTrace()
	}
	return SpanContext{
		TraceID:  sc.TraceID,
		SpanID:   randHex(spanIDBytes),
		ParentID: sc.SpanID,
	}
}

// IsZero reports whether sc carries no trace identity.
func (sc SpanContext) IsZero() bool {
	return sc.TraceID == "" && sc.SpanID == ""
}

// randHex returns n cryptographically random bytes as lowercase hex. It panics
// only if the system CSPRNG fails, which is a non-recoverable platform fault —
// this is process initialisation, not the MCP request boundary, so the panic
// does not cross the MCP boundary (CLAUDE.md §13).
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("dockyard/runtime/obs: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// newEventID returns a fresh 128-bit event identifier as lowercase hex. It is
// distinct from the trace/span IDs: an Event.ID is unique per event, whereas a
// span-id is shared by a start/end pair.
func newEventID() string { return randHex(eventIDBytes) }

// isHex reports whether s is exactly n lowercase-hex characters.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// isTraceID reports whether s is a well-formed W3C trace-id (32 lowercase hex).
func isTraceID(s string) bool { return isHex(s, traceIDBytes*2) }

// isSpanID reports whether s is a well-formed W3C span-id (16 lowercase hex).
func isSpanID(s string) bool { return isHex(s, spanIDBytes*2) }
