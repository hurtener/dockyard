package obs

import "encoding/json"

// This file defines the per-kind typed payloads carried in [Event.Payload]
// (RFC §11.2, brief 05 §3.2). Each payload is JSON-encoded into the Event's
// payload field; a consumer decodes the concrete type per [Event.Kind]. The
// payload shapes are part of the obs/v1 contract and pinned by golden tests.

// ToolCallPayload is the payload of a [KindToolCall] event. Tool input/output
// capture defaults to shape + size only — Input/Output are nil unless
// full-content capture is explicitly opted in and redaction-aware (CLAUDE.md
// §7, RFC §11.2). InputShape/OutputShape and the byte counts are the
// always-present default capture.
type ToolCallPayload struct {
	// Tool is the registered tool name.
	Tool string `json:"tool"`
	// Transport is the MCP transport the call arrived on: stdio | http | inmem.
	Transport string `json:"transport,omitempty"`
	// Client is the client name from the initialize handshake, when known.
	Client string `json:"client,omitempty"`

	// InputShape is the structural fingerprint of the tool input (see [Shape]).
	// It is the default, content-free capture.
	InputShape *ValueShape `json:"input_shape,omitempty"`
	// OutputShape is the structural fingerprint of the tool output.
	OutputShape *ValueShape `json:"output_shape,omitempty"`

	// Input is the full tool input. It is nil under the default shape+size
	// policy; it is populated only when full-content capture is opted in and
	// redaction has been applied (CapturePolicyFull).
	Input json.RawMessage `json:"input,omitempty"`
	// Output is the full tool output, under the same opt-in policy as Input.
	Output json.RawMessage `json:"output,omitempty"`

	// ContractOK reports whether the input/output validated against the
	// generated contract schema (P1). A nil value means "not checked".
	ContractOK *bool `json:"contract_ok,omitempty"`
}

// ResourceReadPayload is the payload of a [KindResourceRead] event.
type ResourceReadPayload struct {
	// URI is the resource URI that was read.
	URI string `json:"uri"`
	// MIME is the served content's MIME type, when known.
	MIME string `json:"mime,omitempty"`
	// Bytes is the size of the served content — a size guardrail signal.
	Bytes int `json:"bytes"`
}

// PromptGetPayload is the payload of a [KindPromptGet] event — a prompts/get
// invocation of a registered MCP Prompt (Phase 28; runtime/server.AddPrompt).
//
// Prompts in MCP are templates the host pulls (rather than tools the model
// pushes); the obs/v1 carrier mirrors the resource.read shape — name + size
// guardrail — rather than the tool.call full input/output capture, because
// a prompt's "input" is a small string-argument map and its "output" is a
// rendered message list rather than a typed contract.
type PromptGetPayload struct {
	// Prompt is the registered prompt name.
	Prompt string `json:"prompt"`
	// Messages is the count of messages in the rendered GetPromptResult.
	Messages int `json:"messages,omitempty"`
	// Bytes is the JSON-serialised size of the rendered messages — a size
	// guardrail signal mirroring [ResourceReadPayload.Bytes].
	Bytes int `json:"bytes,omitempty"`
}

// AppLoadPayload is the payload of a [KindAppLoad] event — a ui:// App resource
// served to a host (RFC §7, brief 05 §3.2).
type AppLoadPayload struct {
	// AppID is the App's identifier (its ui:// URI is ResourceURI).
	AppID string `json:"app_id,omitempty"`
	// ResourceURI is the ui:// URI of the served App resource.
	ResourceURI string `json:"resource_uri"`
	// MIME is the served MIME type — text/html;profile=mcp-app for an App.
	MIME string `json:"mime,omitempty"`
	// Bytes is the size of the served HTML bundle.
	Bytes int `json:"bytes"`
}

// AppBridgePayload is the payload of a [KindAppBridge] event — the ui/initialize
// bridge handshake state. Dockyard sees only its half of the iframe bridge
// (brief 05 §2.5): "served the resource, handshake received or not".
type AppBridgePayload struct {
	// ResourceURI is the ui:// URI of the App whose bridge this concerns.
	ResourceURI string `json:"resource_uri"`
	// BridgeReady reports whether the ui/initialize handshake completed.
	BridgeReady bool `json:"bridge_ready"`
}

// TaskProgressPayload is the payload of a [KindTaskProgress] event — a
// long-running task lifecycle/progress point (RFC §8).
type TaskProgressPayload struct {
	// TaskID is the task identifier.
	TaskID string `json:"task_id"`
	// Status is the task's lifecycle status at this point.
	Status string `json:"status,omitempty"`
	// Message is an optional human-readable progress note.
	Message string `json:"message,omitempty"`
	// Tool is the task-augmented tool name, when the task wraps a tools/call.
	Tool string `json:"tool,omitempty"`
	// Fraction is the task's completion fraction in [0,1] at a mid-flight
	// progress point — set on the [PhaseProgress] events that a
	// TaskHandle.Progress call emits, omitted on lifecycle (start/end) events
	// and on status-only updates (TaskHandle.Status). A consumer (the bridge's
	// task-progress channel, RFC §8.4) renders it as a percentage. Additive,
	// optional field on the obs/v1 contract (v1.3 wave B — D-171); a consumer
	// that does not read it is unaffected.
	Fraction *float64 `json:"fraction,omitempty"`
}

// ServerLifecyclePayload is the payload of a [KindServerLifecycle] event.
type ServerLifecyclePayload struct {
	// State is the lifecycle transition: "starting" | "stopped".
	State string `json:"state"`
	// ServerName is the human-facing server name.
	ServerName string `json:"server_name,omitempty"`
	// Version is the server's semantic version.
	Version string `json:"version,omitempty"`
	// Transport is the transport the server is serving over, when known.
	Transport string `json:"transport,omitempty"`
	// Tools is the count of registered tools.
	Tools int `json:"tools,omitempty"`
}

// LogPayload is the payload of a [KindLog] event — the obs/v1 carrier for an
// MCP notifications/message log record. The actual MCP logging→obs/v1 bridge is
// Phase 16 (RFC §11.3); the payload shape is part of the obs/v1 contract now so
// Phase 16 is purely a new event source.
type LogPayload struct {
	// Level is the RFC 5424 severity (debug, info, warning, error, …).
	Level string `json:"level"`
	// Logger is the optional logger name from the MCP log record.
	Logger string `json:"logger,omitempty"`
	// Message is the log message.
	Message string `json:"message"`
}

// marshalPayload encodes a per-kind payload value. A nil payload yields a nil
// RawMessage so the Event omits the field. A marshal failure yields nil rather
// than a panic — observability never fails a request (P2).
func marshalPayload(p any) json.RawMessage {
	if p == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return b
}
