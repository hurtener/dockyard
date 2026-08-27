package tool

import "github.com/hurtener/dockyard/runtime/tasks"

// Result is what a Dockyard tool handler returns: a small typed value the
// runtime maps onto a standard MCP CallToolResult (RFC §6.3).
//
//   - Text becomes content[] — model-facing text that enters the LLM context.
//   - Structured becomes structuredContent — the typed, UI-facing payload, kept
//     out of the model context. Its shape is the tool's generated output schema.
//   - Meta becomes _meta — extension metadata (for example a viewUUID once the
//     Apps layer lands).
//
// Phase 04 ships this split so the contract-first builder is usable end to end.
// The full handler-runtime semantics — oversized-payload detection, the
// content/structuredContent validation warnings — are Phase 08's (RFC §6.3).
type Result[Out any] struct {
	// Text is the model-facing text rendered into content[].
	Text string
	// Structured is the typed, UI-facing output rendered into
	// structuredContent. Its type is the tool's output contract.
	Structured Out
	// StructuredPresent forces structuredContent to be emitted when Structured
	// is a nil pointer, map, slice, or interface. Such a typed
	// nil is otherwise absent; forcing it emits explicit JSON null and its MCP
	// JSON text fallback while preserving the typed Out contract.
	StructuredPresent bool
	// Meta is optional extension metadata rendered into _meta.
	Meta map[string]any
	// InputRequests asks the client to fulfill typed requests and retry this
	// tool. It must be empty for a complete result.
	InputRequests map[string]InputRequest
	// RequestState is opaque continuation state echoed on the retry.
	RequestState RequestState
	// CreatedTask designates this call's result as an accepted task. It is a
	// domain value; the server edge chooses the versioned wire representation.
	CreatedTask *tasks.CreatedTask
}
