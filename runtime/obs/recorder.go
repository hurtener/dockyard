package obs

import (
	"context"
	"encoding/json"
	"time"
)

// Recorder is the headless emit helper a subsystem uses to record obs/v1 events
// without hand-assembling an [Event]. It binds a server identity and an
// [Emitter] once; every event it builds carries [SchemaVersion], a fresh event
// ID, a UTC timestamp, and the server identity automatically — so a call site
// supplies only what is genuinely call-specific.
//
// A Recorder is the ONLY thing runtime/server, runtime/apps, and runtime/tasks
// touch to observe: they EMIT through it; nothing reads another subsystem's
// internals (P2, CLAUDE.md §6). A Recorder is a reusable concurrent artifact
// and safe for use from many goroutines (CLAUDE.md §5).
//
// A nil *Recorder is valid and discards every event — a subsystem constructed
// without observability calls the same methods unconditionally.
type Recorder struct {
	emitter  Emitter
	serverID string
	policy   CapturePolicy
	redactor Redactor
	now      func() time.Time // injectable clock; nil → time.Now
}

// RecorderOption tunes a [Recorder] at construction.
type RecorderOption func(*Recorder)

// WithCapturePolicy sets the tool input/output capture policy. The default is
// [CapturePolicyShape] — shape + size only (CLAUDE.md §7). [CapturePolicyFull]
// is honoured only when [WithRedactor] also supplies a redactor.
func WithCapturePolicy(p CapturePolicy) RecorderOption {
	return func(r *Recorder) { r.policy = p }
}

// WithRedactor supplies the redaction-aware [Redactor] that [CapturePolicyFull]
// requires. Without it, full-content capture degrades to shape+size.
func WithRedactor(rd Redactor) RecorderOption {
	return func(r *Recorder) { r.redactor = rd }
}

// withClock injects a deterministic clock — test-only.
func withClock(now func() time.Time) RecorderOption {
	return func(r *Recorder) { r.now = now }
}

// NewRecorder binds emitter and serverID into a [Recorder]. A nil emitter is
// promoted to [NopEmitter] so the returned Recorder is always safe to call.
func NewRecorder(emitter Emitter, serverID string, opts ...RecorderOption) *Recorder {
	if emitter == nil {
		emitter = NopEmitter{}
	}
	r := &Recorder{emitter: emitter, serverID: serverID}
	for _, o := range opts {
		o(r)
	}
	return r
}

// timestamp returns the current time, honouring an injected clock.
func (r *Recorder) timestamp() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

// emit assembles the common Event envelope and forwards it. A nil Recorder
// discards the event. Phase=end events carry a duration.
//
// SessionID is stamped from the in-flight MCP session id when [WithSession]
// threaded one onto ctx (R5 — depth-audit remediation; D-120). A transport
// (or a handler edge) that knows the session calls obs.WithSession on its
// handler context and every event the unit of work emits then carries
// SessionID — the same correlation seam [WithSpan] uses for parent-span
// linkage. The field stays optional: ctx without a session emits an event
// with SessionID == "" and the obs/v1 contract's `session_id,omitempty`
// drops it from the wire.
func (r *Recorder) emit(ctx context.Context, sc SpanContext, kind EventKind, phase Phase, payload any, dur *int64, errInfo *ErrorInfo) {
	if r == nil {
		return
	}
	e := Event{
		SchemaVersion: SchemaVersion,
		ID:            newEventID(),
		Timestamp:     r.timestamp(),
		ServerID:      r.serverID,
		SessionID:     sessionFromContext(ctx),
		TraceID:       sc.TraceID,
		SpanID:        sc.SpanID,
		ParentSpanID:  sc.ParentID,
		Kind:          kind,
		Phase:         phase,
		Payload:       marshalPayload(payload),
		DurationMS:    dur,
		Error:         errInfo,
	}
	r.emitter.Emit(ctx, e)
}

// sessionKey is the context key under which an MCP session identity is
// threaded so [Recorder.emit] can stamp it onto every event emitted from
// inside the unit of work.
type sessionKey struct{}

// WithSession returns a copy of ctx carrying an MCP session identity. Every
// obs/v1 event emitted from a Recorder on ctx then carries SessionID equal
// to sessionID — the correlation seam by which a transport-layer session
// identity reaches the emit sites (R5 — depth-audit remediation; D-120).
//
// runtime/server stamps this on the tool-handler context (alongside [WithSpan])
// from req.Session.ID(); the same threading lands on the resource-handler edge.
// A nil/empty sessionID is a no-op and leaves ctx unchanged — an
// out-of-request emit site (a server.lifecycle event, an out-of-request
// log record) is correctly emitted with no session.
func WithSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, sessionID)
}

// sessionFromContext extracts a session id stamped by [WithSession], or ""
// when none is present. Recorder.emit calls this on every event to populate
// the public obs/v1 SessionID wire field.
func sessionFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sessionKey{}).(string)
	return v
}

// spanKey is the context key under which an in-flight unit of work threads its
// [SpanContext] so a nested emit site can correlate to the enclosing span.
type spanKey struct{}

// inboundTraceKey is the context key under which a transport-layer propagator
// (e.g. the W3C TraceContext extractor on the HTTP transport) threads the
// inbound trace identity it parsed from the request — `traceparent` /
// `tracestate`. NewTraceFromContext consults it so a handler-edge span
// inherits the calling agent's trace rather than minting an unrelated root
// (R5 — depth-audit remediation; D-122). The carrier is a SpanContext whose
// SpanID is the inbound span id — a Dockyard handler's span then nests under
// it as a child of the caller, satisfying the RFC §11.2 claim that a
// Dockyard server's spans nest under a calling Harbor agent's `execute_tool`
// span.
type inboundTraceKey struct{}

// WithInboundTrace returns a copy of ctx carrying parent as the trace identity
// extracted by a transport-layer propagator from an inbound request. A
// handler-edge call site that begins a new unit of work calls
// [NewTraceFromContext] (not [NewTrace]) so the unit of work inherits this
// parent when one is present. A zero-value parent is a no-op — leaves ctx
// unchanged — so a transport that finds no traceparent simply skips the call.
func WithInboundTrace(ctx context.Context, parent SpanContext) context.Context {
	if parent.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, inboundTraceKey{}, parent)
}

// InboundTraceFromContext returns the inbound parent trace stamped by
// [WithInboundTrace], and ok=false when ctx carries none.
func InboundTraceFromContext(ctx context.Context) (sc SpanContext, ok bool) {
	v, ok := ctx.Value(inboundTraceKey{}).(SpanContext)
	return v, ok
}

// NewTraceFromContext is the handler-edge counterpart of [NewTrace]: when ctx
// carries an inbound trace stamped by [WithInboundTrace], the returned span
// inherits the parent's TraceID (preserving the call-chain identity) and is
// a child of the parent's span; otherwise it falls back to [NewTrace] — a
// fresh root trace. Use it at any handler-edge call site that opens a new
// obs/v1 unit of work, so a cross-process call from a Harbor agent (or any
// other W3C-compliant propagator) naturally nests Dockyard's spans under
// the caller's (R5; D-122).
func NewTraceFromContext(ctx context.Context) SpanContext {
	if parent, ok := InboundTraceFromContext(ctx); ok {
		return parent.Child()
	}
	return NewTrace()
}

// WithSpan returns a copy of ctx carrying sc as the in-flight span. A
// subsystem that opens a span (a tools/call, a resources/read) stamps it here
// so any obs/v1 event emitted from *inside* that unit of work — most notably a
// handler-emitted `log` event via the MCP-logging bridge — can derive a child
// span of the enclosing one rather than minting an unrelated fresh trace.
//
// This is the correlation seam the Wave 6 checkpoint item S1 closes: before
// it, a `log` event emitted inside a tool handler was not trace-correlated to
// its `tool.call`. A zero-value sc leaves ctx unchanged. It mirrors
// [WithSession]: the context is the carrier, the emit site reads it.
func WithSpan(ctx context.Context, sc SpanContext) context.Context {
	if sc.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, spanKey{}, sc)
}

// SpanFromContext returns the in-flight [SpanContext] stamped by [WithSpan],
// and ok=false when ctx carries none. An emit site that wants to nest under an
// enclosing span calls SpanFromContext and, when ok, uses sc.Child(); when not
// ok it begins a fresh trace with [NewTrace].
func SpanFromContext(ctx context.Context) (sc SpanContext, ok bool) {
	v, ok := ctx.Value(spanKey{}).(SpanContext)
	return v, ok
}

// ChildOrNewTrace returns a child span of the in-flight span carried by ctx,
// or a fresh root trace when ctx carries none. It is the one-call form of the
// "correlate if you can, else start a trace" pattern an emit site nested
// inside a possibly-instrumented unit of work uses.
func ChildOrNewTrace(ctx context.Context) SpanContext {
	if sc, ok := SpanFromContext(ctx); ok {
		return sc.Child()
	}
	return NewTrace()
}

// --- tool.call ---------------------------------------------------------------

// ToolCall records the start of a tools/call and returns a function that
// records its end. The two events share a span; the caller invokes the returned
// function once the tool returns, passing the typed input/output (for shape
// capture) and any error. Usage:
//
//	end := rec.ToolCall(ctx, sc, "search", "stdio")
//	out, err := handler(ctx, in)
//	end(inputJSON, outputJSON, err)
//
// The returned closure is safe to call exactly once.
func (r *Recorder) ToolCall(ctx context.Context, sc SpanContext, tool, transport string) func(input, output json.RawMessage, err error) {
	if r == nil {
		return func(json.RawMessage, json.RawMessage, error) {}
	}
	start := r.timestamp()
	r.emit(ctx, sc, KindToolCall, PhaseStart, ToolCallPayload{
		Tool:       tool,
		Transport:  transport,
		Client:     "",
		InputShape: nil, // start carries no payload shape; the end event does
	}, nil, nil)
	return func(input, output json.RawMessage, err error) {
		inShape, inFull := captureValue(input, r.policy, r.redactor)
		outShape, outFull := captureValue(output, r.policy, r.redactor)
		dur := durMS(start, r.timestamp())
		r.emit(ctx, sc, KindToolCall, PhaseEnd, ToolCallPayload{
			Tool:        tool,
			Transport:   transport,
			InputShape:  &inShape,
			OutputShape: &outShape,
			Input:       inFull,
			Output:      outFull,
		}, &dur, errorInfo(err))
	}
}

// --- resource.read -----------------------------------------------------------

// ResourceRead records the start of a resources/read and returns a function
// that records its end with the served MIME type and byte size.
func (r *Recorder) ResourceRead(ctx context.Context, sc SpanContext, uri string) func(mime string, bytes int, err error) {
	if r == nil {
		return func(string, int, error) {}
	}
	start := r.timestamp()
	r.emit(ctx, sc, KindResourceRead, PhaseStart, ResourceReadPayload{URI: uri}, nil, nil)
	return func(mime string, bytes int, err error) {
		dur := durMS(start, r.timestamp())
		r.emit(ctx, sc, KindResourceRead, PhaseEnd, ResourceReadPayload{
			URI:   uri,
			MIME:  mime,
			Bytes: bytes,
		}, &dur, errorInfo(err))
	}
}

// --- prompt.get --------------------------------------------------------------

// PromptGet records the start of a prompts/get and returns a function that
// records its end with the rendered message count and serialized byte size.
// It mirrors [ResourceRead]'s shape: a prompts/get is a host-pulled
// template render whose interesting observability signals are name + size,
// not a typed input/output capture (Phase 28; runtime/server.AddPrompt).
//
// The returned closure is safe to call exactly once.
func (r *Recorder) PromptGet(ctx context.Context, sc SpanContext, prompt string) func(messages, bytes int, err error) {
	if r == nil {
		return func(int, int, error) {}
	}
	start := r.timestamp()
	r.emit(ctx, sc, KindPromptGet, PhaseStart, PromptGetPayload{Prompt: prompt}, nil, nil)
	return func(messages, bytes int, err error) {
		dur := durMS(start, r.timestamp())
		r.emit(ctx, sc, KindPromptGet, PhaseEnd, PromptGetPayload{
			Prompt:   prompt,
			Messages: messages,
			Bytes:    bytes,
		}, &dur, errorInfo(err))
	}
}

// --- app.load ----------------------------------------------------------------

// AppLoad records a point-in-time app.load event: a ui:// App resource served
// to a host (RFC §7).
func (r *Recorder) AppLoad(ctx context.Context, sc SpanContext, p AppLoadPayload) {
	r.emit(ctx, sc, KindAppLoad, PhaseEmit, p, nil, nil)
}

// AppBridge records a point-in-time app.bridge event — the ui/initialize bridge
// handshake state.
func (r *Recorder) AppBridge(ctx context.Context, sc SpanContext, p AppBridgePayload) {
	r.emit(ctx, sc, KindAppBridge, PhaseEmit, p, nil, nil)
}

// --- task.progress -----------------------------------------------------------

// TaskEvent records a task lifecycle/progress event. phase is one of
// [PhaseStart] (task created), [PhaseProgress] (an intermediate point), or
// [PhaseEnd] (terminal). On a terminal failure pass err.
func (r *Recorder) TaskEvent(ctx context.Context, sc SpanContext, phase Phase, p TaskProgressPayload, err error) {
	r.emit(ctx, sc, KindTaskProgress, phase, p, nil, errorInfo(err))
}

// --- server.lifecycle --------------------------------------------------------

// ServerLifecycle records a point-in-time server.lifecycle event.
func (r *Recorder) ServerLifecycle(ctx context.Context, sc SpanContext, p ServerLifecyclePayload) {
	r.emit(ctx, sc, KindServerLifecycle, PhaseEmit, p, nil, nil)
}

// --- log ---------------------------------------------------------------------

// Log records a point-in-time obs/v1 log event — the obs/v1 carrier for an MCP
// notifications/message log record. It is the emit side of the Phase 16 MCP
// logging → obs/v1 bridge (RFC §11.3): server.LogBridge calls Log so a server
// log record surfaces as an obs/v1 log event, in addition to the standard MCP
// notifications/message a Dockyard server still emits. obs/v1 is a one-way
// event stream, never a back channel (P2, CLAUDE.md §6).
func (r *Recorder) Log(ctx context.Context, sc SpanContext, p LogPayload) {
	r.emit(ctx, sc, KindLog, PhaseEmit, p, nil, nil)
}

// --- helpers -----------------------------------------------------------------

// durMS returns the elapsed milliseconds between start and end, clamped at 0.
func durMS(start, end time.Time) int64 {
	d := end.Sub(start).Milliseconds()
	if d < 0 {
		return 0
	}
	return d
}

// errorInfo lowers a Go error onto an [ErrorInfo], or nil for a nil error. The
// default Type is "handler_error"; a richer classification is a later phase's
// concern (the obs/v1 contract carries the field now).
func errorInfo(err error) *ErrorInfo {
	if err == nil {
		return nil
	}
	return &ErrorInfo{Type: "handler_error", Message: err.Error()}
}
