// Package apps is the Dockyard server-side MCP Apps extension layer
// (io.modelcontextprotocol/ui, SEP-1865, spec revision 2026-01-26 — RFC §7).
//
// An MCP App is not a new protocol primitive: it is a convention layered on a
// tool and a resource (brief 01 §2.1). This package implements the server half
// of that convention (RFC §7.1):
//
//   - it registers a ui:// resource carrying the App's HTML bundle, served with
//     MIME type text/html;profile=mcp-app — the only MVP resource type;
//   - it attaches _meta.ui to the resources/read response — CSP, permissions,
//     domain, prefersBorder — through a single choke point, because the MCP
//     Apps spec reads CSP and domain from the read *response*, not only the
//     static resource declaration (brief 01 §2.2, RFC §7.1);
//   - it builds the _meta.ui object that links a tool definition to its ui://
//     resource — the nested {resourceUri, visibility} form only, never the
//     deprecated flat tool-UI _meta form;
//   - it advertises the io.modelcontextprotocol/ui extension capability with
//     mimeTypes ["text/html;profile=mcp-app"] (RFC §7.1).
//
// When no CSP is declared the encoded policy is deny-by-default — zero external
// origins — which is why generated apps default to single-file HTML bundles
// (RFC §7.4). A host may further restrict the policy but never loosen it.
//
// Graceful degradation is mandatory and automatic: nothing in this package
// gates tool or resource registration on the host advertising the extension.
// A host that does not negotiate io.modelcontextprotocol/ui simply ignores
// _meta.ui and gets a fully working plain MCP server (RFC §7.1, §7.5).
//
// Every MCP extension wire shape this package needs — the _meta.ui tool and
// resource objects, the extensions-capability block — is produced by
// internal/protocolcodec. Package apps constructs no raw extension wire JSON
// itself, preserving the protocolcodec isolation seam (P3, RFC §5.4).
//
// Phase 10 adds convention-based UI auto-discovery and the embed pipeline on
// top of this surface, in new files that compose Register/App rather than
// rewriting them (RFC §7.6, §14):
//
//   - Discover walks the web/src/apps/ convention directory and lifts every
//     .svelte file into a DiscoveredApp — no manual registration call;
//   - Bundle is an embed.FS-backed view of the built Svelte UI (//go:embed
//     all:dist); one embed.FS backs the ui:// MCP resource handler;
//   - RegisterDiscovered registers a DiscoveredApp as a ui:// resource by
//     composing Register, reading its HTML from the embedded Bundle.
//
// The discovered tool↔UI wiring is written back into dockyard.app.yaml by
// internal/manifest.WriteDiscoveredApps, so the convention never hides the
// architecture (RFC §7.6).
//
// _meta.ui.domain is the host-supplied dedicated origin (App.Domain), carried
// VERBATIM — the host mints the value and a server copies it; Dockyard never
// synthesises a host's signed origin (D-176, supersedes D-062/D-063). The
// pluggable host-profile seam (hostprofile.go) is retained for a future
// host-blessed transform but ships only the generic verbatim profile.
//
// Out of scope for this package: the Svelte bridge shell (the View-half ui/
// dialect).
package apps
