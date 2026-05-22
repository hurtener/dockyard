package obs

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the obs/v1 schema identifier carried by every [Event]. It is
// a public, versioned contract (RFC §11.3): a change to the [Event] JSON shape
// bumps this value, is documented, and is never silent (CLAUDE.md §8). The
// golden tests in this package pin the serialized shape so an accidental change
// fails CI.
const SchemaVersion = "dockyard.obs/v1"

// EventKind classifies an [Event]. The kinds cover the tool, resource, prompt,
// app, task, host-compat, log, and server-lifecycle surfaces of a Dockyard
// server (RFC §11.2, brief 05 §3.1). The set is closed for obs/v1: a new kind
// is a versioned addition.
type EventKind string

const (
	// KindToolCall is the tools/call lifecycle of a contract-first tool.
	KindToolCall EventKind = "tool.call"
	// KindResourceRead is a resources/read of a registered resource.
	KindResourceRead EventKind = "resource.read"
	// KindPromptGet is a prompts/get of a registered prompt.
	KindPromptGet EventKind = "prompt.get"
	// KindAppLoad is a ui:// App resource served to a host (RFC §7).
	KindAppLoad EventKind = "app.load"
	// KindAppBridge is the ui/initialize bridge handshake — bridge up/down.
	KindAppBridge EventKind = "app.bridge"
	// KindUserAction is an action dispatched from an App UI.
	KindUserAction EventKind = "app.user_action"
	// KindHostCompat is a detected host capability or incompatibility.
	KindHostCompat EventKind = "host.compat"
	// KindLog bridges an MCP notifications/message log record (RFC §11.3 — the
	// bridge itself is Phase 16; the kind is part of the obs/v1 contract now).
	KindLog EventKind = "log"
	// KindServerLifecycle is a server start/stop or capability negotiation.
	KindServerLifecycle EventKind = "server.lifecycle"
	// KindTaskProgress is a long-running task lifecycle/progress event (RFC §8;
	// Tasks is V1 scope so task events are part of obs/v1 — brief 05 Q-8).
	KindTaskProgress EventKind = "task.progress"
)

// valid reports whether k is a known obs/v1 event kind.
func (k EventKind) valid() bool {
	switch k {
	case KindToolCall, KindResourceRead, KindPromptGet, KindAppLoad,
		KindAppBridge, KindUserAction, KindHostCompat, KindLog,
		KindServerLifecycle, KindTaskProgress:
		return true
	default:
		return false
	}
}

// Phase is the lifecycle position of an [Event]. A start/end pair brackets a
// duration; progress marks an intermediate point of a long-running task; emit
// is a point-in-time event with no paired counterpart (brief 05 §3.1).
type Phase string

const (
	// PhaseStart opens a lifecycle — e.g. a tools/call has begun.
	PhaseStart Phase = "start"
	// PhaseEnd closes a lifecycle; the event carries DurationMS.
	PhaseEnd Phase = "end"
	// PhaseProgress is an intermediate point of a long-running task.
	PhaseProgress Phase = "progress"
	// PhaseEmit is a point-in-time event with no paired counterpart.
	PhaseEmit Phase = "emit"
)

// valid reports whether p is a known obs/v1 phase.
func (p Phase) valid() bool {
	switch p {
	case PhaseStart, PhaseEnd, PhaseProgress, PhaseEmit:
		return true
	default:
		return false
	}
}

// Event is the canonical obs/v1 observability event — the ONLY type the
// inspector and the post-V1 console consume (RFC §11.2). No raw runtime or SDK
// type leaks through it (P2/P3). The JSON shape is a stable, versioned contract
// pinned by golden tests; field order in the struct is the documented wire
// order.
type Event struct {
	// SchemaVersion is always [SchemaVersion] for obs/v1 emitters. A consumer
	// keys parsing on it.
	SchemaVersion string `json:"schema_version"`
	// ID uniquely identifies this event. It is a crypto-random 128-bit hex
	// string (see newEventID) — distinct from the W3C trace/span IDs.
	ID string `json:"id"`
	// Timestamp is when the event was recorded, in UTC.
	Timestamp time.Time `json:"timestamp"`

	// ServerID is the stable identity of the emitting server.
	ServerID string `json:"server_id"`
	// SessionID is the MCP session the event belongs to, when known.
	SessionID string `json:"session_id,omitempty"`

	// TraceID is the W3C Trace Context trace-id (16 bytes, 32 lowercase hex)
	// correlating a whole call chain. A Dockyard server's spans nest natively
	// under a calling Harbor agent's execute_tool span (RFC §11.2).
	TraceID string `json:"trace_id"`
	// SpanID is the W3C Trace Context span-id (8 bytes, 16 lowercase hex) of
	// this unit of work.
	SpanID string `json:"span_id"`
	// ParentSpanID is the span-id of the enclosing span, when there is one.
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// Kind classifies the event (see [EventKind]).
	Kind EventKind `json:"kind"`
	// Phase is the lifecycle position (see [Phase]).
	Phase Phase `json:"phase"`
	// Payload is the kind-specific typed payload, JSON-encoded. The concrete
	// Go shapes are in payload.go; a consumer decodes per Kind.
	Payload json.RawMessage `json:"payload,omitempty"`

	// DurationMS is the elapsed milliseconds of a completed unit of work. It is
	// set on Phase=end events and omitted otherwise.
	DurationMS *int64 `json:"duration_ms,omitempty"`
	// Error is set when the unit of work failed. ErrorInfo.Silent flags a
	// protocol-masked failure — the class of bug stdio transport hides
	// (brief 05 §2.2, the Sentry insight).
	Error *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo describes a failure carried by an [Event]. It lowers cleanly onto
// the OTel error.type attribute (RFC §11.3); the Phase 16 OTel adapter consumes
// it without obs needing an OTel dependency.
type ErrorInfo struct {
	// Type is a stable, low-cardinality error class — it maps to OTel
	// error.type. Example: "handler_error", "validation_error".
	Type string `json:"type"`
	// Message is the human-readable error detail.
	Message string `json:"message"`
	// Retryable hints whether retrying the operation could succeed.
	Retryable bool `json:"retryable,omitempty"`
	// Silent flags a protocol-masked failure: a failure the MCP transport
	// would otherwise hide (e.g. an error swallowed on the stdio pipe). This is
	// a first-class signal — the inspector surfaces it prominently.
	Silent bool `json:"silent,omitempty"`
}

// valid reports whether e is a structurally well-formed obs/v1 event. An
// emitter calls it before fan-out so a malformed event never reaches a
// consumer; a driver may rely on a received Event being valid.
func (e Event) valid() bool {
	if e.SchemaVersion != SchemaVersion {
		return false
	}
	if e.ID == "" || e.ServerID == "" {
		return false
	}
	if !isTraceID(e.TraceID) || !isSpanID(e.SpanID) {
		return false
	}
	if e.ParentSpanID != "" && !isSpanID(e.ParentSpanID) {
		return false
	}
	if !e.Kind.valid() || !e.Phase.valid() {
		return false
	}
	return true
}
