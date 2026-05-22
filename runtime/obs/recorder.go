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
func (r *Recorder) emit(ctx context.Context, sc SpanContext, kind EventKind, phase Phase, payload any, dur *int64, errInfo *ErrorInfo) {
	if r == nil {
		return
	}
	e := Event{
		SchemaVersion: SchemaVersion,
		ID:            newEventID(),
		Timestamp:     r.timestamp(),
		ServerID:      r.serverID,
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

// SessionFromContext is the seam by which a transport-layer session identity is
// threaded onto events. Phase 15 leaves it unset (the engine/server do not yet
// carry a per-call session id into the emit sites); Phase 16's transports
// populate it. Kept here so the wire field is part of the obs/v1 contract now.
type sessionKey struct{}

// WithSession returns a copy of ctx carrying an MCP session identity that a
// later phase's emit sites can stamp onto events.
func WithSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, sessionID)
}

// sessionFromContext extracts a session id stamped by [WithSession], or "".
func sessionFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sessionKey{}).(string)
	return v
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
		_ = sessionFromContext(ctx)
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
