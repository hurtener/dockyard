package tool

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
	// Meta is optional extension metadata rendered into _meta.
	Meta map[string]any
}
