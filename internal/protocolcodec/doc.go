// Package protocolcodec is Dockyard's single isolation seam for every MCP
// extension wire format.
//
// # Why this package exists (RFC §5.4, §16; binding property P3)
//
// The MCP protocol and its extensions move independently and fast. Confining
// every extension wire format — the exact _meta key shapes, the capability
// blocks, the Tasks method envelopes — to one internal package makes a spec
// bump a localized, regenerate-and-diff change rather than a cross-cutting
// refactor. Nothing outside internal/protocolcodec is permitted to import or
// construct raw MCP extension wire types; handler-facing and manifest-facing
// Dockyard APIs deal only in Dockyard's own domain types and convert at this
// seam. AGENTS.md §10 and §13 make that boundary binding; it is mechanically
// enforced by TestNoRawWireTypeImportsOutsideSeam in boundary_test.go.
//
// # What lives here
//
//   - Versioned codecs (see [Codec], [CodecFor]) keyed on the negotiated MCP
//     protocolVersion string. A codec encodes Dockyard domain types into the
//     wire shape for its version and decodes wire shapes back. Deprecated
//     shapes — notably the flat _meta["ui/resourceUri"] form from the MCP Apps
//     spec — are tolerated on read but NEVER emitted (RFC §16 item 3).
//
//   - Typed _meta accessors for the MCP Apps extension
//     (io.modelcontextprotocol/ui, spec revision 2026-01-26, SEP-1865) and the
//     MCP Tasks extension (io.modelcontextprotocol/tasks, experimental,
//     SEP-1686/2663). Because _meta is untyped on the wire (map[string]any),
//     these accessors make extension-metadata bugs surface in Dockyard's own
//     validation rather than at runtime inside a host (brief 03 R7).
//
// # Vendored specs
//
// The wire shapes implemented here are pinned to vendored spec snapshots under
// docs/specifications/ (mcp-apps-2026-01-26.mdx and
// mcp-tasks-experimental.schema.ts). A spec revision is a deliberate, reviewed
// update of those files followed by a regenerate-and-diff of this package.
package protocolcodec
