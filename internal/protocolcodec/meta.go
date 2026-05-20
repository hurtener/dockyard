package protocolcodec

// Meta is the raw, untyped MCP `_meta` object as it appears on the wire.
//
// It is structurally identical to the go-sdk's `mcp.Meta` (`map[string]any`,
// JSON-tagged `_meta`); protocolcodec deliberately keeps its own alias so the
// seam has no compile-time dependency on a specific SDK release for a type this
// trivial. A `nil` Meta marshals to an absent `_meta` field.
//
// Meta is only ever produced or consumed at this seam. Code outside
// internal/protocolcodec works with Dockyard domain types ([AppsToolMeta],
// [AppsResourceMeta], [TaskMeta], …) and converts through a [Codec].
type Meta map[string]any

// Extension identifiers, exactly as registered in the MCP capability registry.
const (
	// ExtensionApps is the MCP Apps extension identifier (SEP-1865).
	ExtensionApps = "io.modelcontextprotocol/ui"
	// ExtensionTasks is the MCP Tasks extension identifier (SEP-1686/2663).
	ExtensionTasks = "io.modelcontextprotocol/tasks"
)

// _meta key constants. These are the literal keys that appear inside a `_meta`
// object on the wire. Defining them once, here, is the whole point of the seam:
// a spec rename is a one-line change in this file.
const (
	// metaKeyUI is the nested MCP Apps metadata object: `_meta.ui`.
	metaKeyUI = "ui"

	// metaKeyUIResourceURIFlat is the DEPRECATED flat MCP Apps form,
	// `_meta["ui/resourceUri"]`. The 2026-01-26 spec marks it deprecated and
	// slated for removal before GA. protocolcodec tolerates it on read and
	// NEVER emits it (RFC §16 item 3; brief 01 §2.3).
	metaKeyUIResourceURIFlat = "ui/resourceUri"

	// metaKeyRelatedTask is the MCP Tasks association key,
	// `_meta["io.modelcontextprotocol/related-task"]`, carried by every
	// request/response/notification tied to a task (brief 02 §2.8).
	metaKeyRelatedTask = "io.modelcontextprotocol/related-task"

	// metaKeyModelImmediateResponse is the provisional MCP Tasks key
	// `_meta["io.modelcontextprotocol/model-immediate-response"]` carried on a
	// CreateTaskResult: a string handed to the model so the host can return
	// control while a task runs (brief 02 §2.7; D-014).
	metaKeyModelImmediateResponse = "io.modelcontextprotocol/model-immediate-response"
)

// MIMETypeApp is the only MCP Apps resource MIME type defined by the MVP spec
// (`text/html;profile=mcp-app`). Routing it through one constant means a future
// profile type is a one-line change (brief 01 §5, sharp edge 5).
const MIMETypeApp = "text/html;profile=mcp-app"

// clone returns a shallow copy of m, or nil if m is nil. Callers mutate the
// copy so a decode never aliases — and never accidentally mutates — a caller's
// map.
func (m Meta) clone() Meta {
	if m == nil {
		return nil
	}
	out := make(Meta, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
