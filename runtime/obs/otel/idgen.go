package otel

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// This file makes a Dockyard OTel span carry the obs/v1 event's OWN W3C
// trace-id and span-id. The OpenTelemetry SDK assigns span IDs through an
// IDGenerator; obs/v1 has already minted spec-shaped W3C IDs on every event
// (RFC §11.2). idGenerator bridges the two: the OTelEmitter stashes the event's
// IDs in the context it passes to Tracer.Start, and the SDK — calling the
// IDGenerator while building the span — gets exactly those IDs back. The result
// is that a Dockyard server's exported spans share identity with its obs/v1
// events and nest natively under a calling Harbor agent's execute_tool span.

// otelIDs is one obs/v1 event's W3C identity in OTel ID types.
type otelIDs struct {
	trace oteltrace.TraceID
	span  oteltrace.SpanID
	// parent is the enclosing span's id, when the obs/v1 event carried a
	// ParentSpanID; the zero SpanID when it did not. It does NOT flow through
	// the IDGenerator — the IDGenerator only assigns the span's own IDs. The
	// parent linkage is established on the start context by the emitter
	// (ContextWithRemoteSpanContext), so this field is carried only for the
	// emitter to read (D-114).
	parent oteltrace.SpanID
}

// hasParent reports whether the event carried a well-formed ParentSpanID.
func (i otelIDs) hasParent() bool {
	return i.parent.IsValid()
}

// idKey is the context key under which the OTelEmitter passes the desired IDs
// to the IDGenerator for the span currently being started.
type idKey struct{}

// idGenerator is an OpenTelemetry sdktrace.IDGenerator that returns the IDs
// stashed in the start context by [idGenerator.withIDs]. With no stashed IDs it
// is never used — the OTelEmitter always stashes IDs before starting a span, so
// every Dockyard span is W3C-correlated; the fallbacks below exist only so the
// generator is a total IDGenerator and never panics.
type idGenerator struct{}

// newIDGenerator constructs the context-keyed IDGenerator.
func newIDGenerator() *idGenerator { return &idGenerator{} }

// withIDs returns a context carrying ids for the IDGenerator to hand back when
// the SDK builds the next span started with that context.
func (g *idGenerator) withIDs(ctx context.Context, ids otelIDs) context.Context {
	return context.WithValue(ctx, idKey{}, ids)
}

// NewIDs returns the trace-id and span-id for a new root span — the obs/v1
// event's own W3C IDs when present. It implements sdktrace.IDGenerator.
func (g *idGenerator) NewIDs(ctx context.Context) (oteltrace.TraceID, oteltrace.SpanID) {
	if ids, ok := ctx.Value(idKey{}).(otelIDs); ok {
		return ids.trace, ids.span
	}
	// Total-function fallback: a random-looking but valid pair. Unreachable in
	// the OTelEmitter's own flow (it always stashes IDs first).
	return oteltrace.TraceID{0x1}, oteltrace.SpanID{0x1}
}

// NewSpanID returns the span-id for a child span — the obs/v1 event's span-id
// when present. It implements sdktrace.IDGenerator.
func (g *idGenerator) NewSpanID(ctx context.Context, _ oteltrace.TraceID) oteltrace.SpanID {
	if ids, ok := ctx.Value(idKey{}).(otelIDs); ok {
		return ids.span
	}
	return oteltrace.SpanID{0x1}
}
