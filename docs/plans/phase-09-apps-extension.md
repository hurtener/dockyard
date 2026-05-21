# Phase 09 — apps-extension

## Summary

Phase 09 ships `runtime/apps` — the server-side MCP Apps extension layer
(`io.modelcontextprotocol/ui`, SEP-1865, spec revision 2026-01-26). It registers
`ui://` resources with MIME `text/html;profile=mcp-app`, attaches `_meta.ui` to
tool definitions (the nested form only) and to `resources/read` responses
(CSP / domain / permissions / `prefersBorder`), advertises the `extensions`
capability, and behaves as a plain MCP server when a host does not advertise the
extension. All extension wire encoding goes through `internal/protocolcodec`.

## RFC anchor

- RFC §7.1 — what Dockyard registers (`ui://` resources, `_meta.ui` on tools and
  on the resource-read response, the `extensions` capability, plain-MCP
  graceful degradation).
- RFC §7.4 — CSP, sandboxing, single-file bundles (deny-by-default CSP when none
  is declared).
- RFC §5.3 — the SDK extension hooks (`AddExtension`, `_meta`) the Apps layer
  attaches through.
- RFC §5.4 — the `protocolcodec` isolation seam (P3).

## Briefs informing this phase

- brief 01 — MCP Apps extension.
- brief 03 — Official Go MCP SDK audit.

## Brief findings incorporated

- **brief 01 §2.2** — "`_meta.ui.csp` and `_meta.ui.domain` are read from the
  `contents[]` entry returned by the resource-read callback — *not* only from
  the static resource declaration." `runtime/apps` threads `_meta.ui` through a
  single choke point (`uiResourceHandler`) so every `resources/read` reply
  carries the correct metadata.
- **brief 01 §2.3** — a tool declares its UI through the nested
  `_meta.ui = {resourceUri, visibility}`; the flat `_meta["ui/resourceUri"]`
  form is deprecated. Dockyard emits only the nested form — enforced by routing
  every encode through `protocolcodec`, which strips the flat key.
- **brief 01 §2.5** — the default CSP applied when `_meta.ui.csp` is omitted is
  deny-by-default. Phase 09 always emits a `_meta.ui` on a UI resource-read
  response; when the App declares no domains the encoded CSP is the
  zero/deny-by-default policy (no external origins), which the host enforces.
- **brief 01 §2.7** — Apps is negotiated through the core `extensions`
  capability; a host that does not advertise `io.modelcontextprotocol/ui` still
  gets a fully working plain MCP server. Phase 09 advertises the capability
  unconditionally and never gates tool registration on it.
- **brief 03 §2.4** — the SDK exposes exactly two hooks: `AddExtension` for
  capability negotiation and first-class `_meta`. `runtime/apps` attaches
  through these via a thin additive seam on `runtime/server`.

## Findings I'm departing from (if any)

- **brief 01 §2.8 / §5 ("per-host capability matrix").** Brief 01 calls for a
  per-host capability matrix feeding compatibility checks. Phase 09 deliberately
  does **not** build one: RFC §7.5 and AGENTS.md §6 settled that Dockyard relies
  on the MCP capability-negotiation handshake and never hardcodes a host matrix.
  Host-specific derivations (e.g. `_meta.ui.domain` signing) are Phase 12's
  pluggable host profiles. Phase 09 only plumbs the `domain` field through
  `_meta.ui`; it never derives it. Filed as **D-049**.

## Goals

- A `runtime/apps` package that registers a tool↔`ui://` resource pair so an
  Apps-capable client can discover it (`tools/list` carries `_meta.ui`,
  `resources/read` returns `text/html;profile=mcp-app` with `_meta.ui`).
- The `extensions` capability `io.modelcontextprotocol/ui` advertised with
  `mimeTypes: ["text/html;profile=mcp-app"]`.
- A resource-read `_meta.ui` choke point: CSP (deny-by-default when none is
  declared), permissions, domain, `prefersBorder` always correctly attached.
- Plain-MCP graceful degradation: a non-Apps host still gets fully working
  tools and resources.
- All `_meta` / capability wire encoding routed through `internal/protocolcodec`
  — no raw extension wire shapes in `runtime/apps` (P3).

## Non-goals

- UI-resource auto-discovery and the `embed.FS` build pipeline — Phase 10.
- The Svelte bridge shell library (`web/bridge`) — Phase 11.
- Host-profile **derivation** of `_meta.ui.domain` (Claude's signed origin) —
  Phase 12. Phase 09 only carries the `domain` field verbatim.
- The `postMessage` `ui/` bridge dialect (client-shaped) — Phase 11 / inspector.
- Manifest-to-app wiring discovery written back into `dockyard.app.yaml` —
  Phase 10.

## Acceptance criteria

- [ ] A tool↔`ui://` resource pair registered via `runtime/apps` is discoverable
      by an Apps-capable client: `tools/list` returns the tool with
      `_meta.ui.resourceUri` (nested form) and `resources/read` returns the App
      HTML with MIME `text/html;profile=mcp-app`.
- [ ] The `resources/read` response carries `_meta.ui`; when the App declares no
      CSP the encoded policy is deny-by-default (no external origins) — RFC §7.4.
- [ ] The server advertises the `extensions` capability
      `io.modelcontextprotocol/ui` with `mimeTypes: ["text/html;profile=mcp-app"]`.
- [ ] A non-Apps host (one that does not advertise the extension) still gets
      fully working tools and resources — graceful degradation.
- [ ] The deprecated flat `_meta["ui/resourceUri"]` form is never emitted on a
      tool definition.
- [ ] No raw MCP extension wire shape is constructed in `runtime/apps`; every
      `_meta` / capability value is produced by `internal/protocolcodec` (P3).
- [ ] `runtime/apps` builds CGo-free; tests pass under `-race`; a concurrent
      resource-read test passes; coverage ≥ 85%.

## Files added or changed

```text
runtime/apps/
  doc.go            # package doc
  apps.go           # App, Registry, Register — the public surface
  csp.go            # CSP / Permissions / domain types + protocolcodec bridge
  capability.go     # extensions-capability advertisement
  apps_test.go
  csp_test.go
  capability_test.go
  concurrency_test.go
runtime/server/
  server.go         # Options.Extensions — extension-capability advertisement
  resource.go       # ResourceDef.Meta, ResourceContent.Meta — _meta plumbing
  tool.go           # ToolDef.Meta — tool-definition _meta plumbing
docs/plans/phase-09-apps-extension.md
docs/decisions.md   # D-047, D-048, D-049
docs/glossary.md
scripts/smoke/phase-09.sh
test/integration/phase09_apps_extension_test.go
```

## Public API surface

```go
package apps

// MIMETypeApp is the only MCP Apps resource MIME type (text/html;profile=mcp-app).
const MIMETypeApp = "text/html;profile=mcp-app"
// ExtensionID is the MCP Apps extension identifier.
const ExtensionID = "io.modelcontextprotocol/ui"

// CSP, Permissions mirror the protocolcodec domain types but are runtime-facing.
type CSP struct { Connect, Resource, Frame, BaseURI []string }
type Permissions struct { Camera, Microphone, Geolocation, ClipboardWrite bool }

// App is a server-side MCP App: a ui:// resource plus its host-facing _meta.ui.
type App struct {
    URI           string        // ui:// resource URI — required
    Name          string        // resource name — required
    Title         string
    Description   string
    HTML          []byte        // the single-file bundle — required
    CSP           CSP
    Permissions   Permissions
    Domain        string        // carried verbatim; derivation is Phase 12
    PrefersBorder *bool
}

// ToolLink links a registered tool to a ui:// resource (nested _meta.ui).
type ToolLink struct {
    ResourceURI string
    Visibility  []string // "model" | "app"; empty = host default
}

// Register registers app as a ui:// resource on s, returning a ToolMeta whose
// AppendTo wires _meta.ui onto a tool definition.
func Register(s *server.Server, app App) error

// ToolMetaFor builds the tool-definition _meta carrying _meta.ui for link.
func ToolMetaFor(link ToolLink) (map[string]any, error)

// AdvertiseExtension returns the server.Options extension entry advertising
// the io.modelcontextprotocol/ui capability.
func ExtensionCapability() (server.ExtensionCapability, error)
```

```go
package server // additive, non-breaking

type Options struct {
    Logger     *slog.Logger
    Extensions []ExtensionCapability // NEW — advertised extensions
}
type ExtensionCapability struct { Name string; Settings json.RawMessage }

type ToolDef struct { Name, Description string; Meta map[string]any } // Meta NEW
type ResourceDef struct { /* … */; Meta map[string]any }              // Meta NEW
type ResourceContent struct { /* … */; Meta map[string]any }          // Meta NEW
```

## Test plan

- **Unit:** `runtime/apps` — `Register` installs a `ui://` resource with the
  App MIME; `resources/read` returns `_meta.ui`; deny-by-default CSP when no
  domains; `ToolMetaFor` produces the nested form only; capability advertisement
  shape; rejects a non-`ui://` URI, empty HTML, the flat form. `runtime/server`
  — additive `Meta` fields surface on `tools/list` / `resources/read`;
  `Options.Extensions` reach the negotiated capabilities.
- **Integration:** `test/integration/phase09_apps_extension_test.go` — a real
  SDK client over a real transport drives `initialize` (asserting the
  `extensions` capability), `tools/list` (asserting `_meta.ui`), and
  `resources/read` (asserting MIME + `_meta.ui` CSP). A non-Apps client variant
  still calls the tool successfully (graceful degradation).
- **Concurrency / golden:** `concurrency_test.go` — N≥16 concurrent
  `resources/read` against one registered App under `-race`, asserting every
  read returns identical content and `_meta` with no data race. The Registry /
  registered handler is the reusable artifact.

## Smoke script additions

- `runtime/apps` package exists and builds CGo-free.
- `runtime/apps` constructs no raw extension wire shape — it imports
  `protocolcodec` and contains no literal `"resourceUri"` / `"connectDomains"`
  JSON keys.
- The flat `ui/resourceUri` form is never emitted (grep guard).
- `runtime/server` exposes the additive `Meta` fields and `Options.Extensions`.
- `runtime/apps` and `runtime/server` tests pass.
- The Phase 09 integration test exists.

## Coverage target

- `runtime/apps` — 85% (new package; the phase's stated target).
- `runtime/server` — additive changes covered by existing + new tests; no
  regression below its 85% target.

## Dependencies

- Phase 07 — `runtime/server` (resource + tool registration, transports).
- Phase 02 — `internal/protocolcodec` (the Apps `_meta` codec).
- Phase 06 — `internal/manifest` (the `apps` section — context for the App shape).

## Risks / open questions

- **RFC §18 Q-5** — auto-derivation of `_meta.ui.domain` is deferred to
  Phase 12; Phase 09 carries the field verbatim. A developer setting `Domain`
  manually in Phase 09 gets it through unchanged.
- The `ResourceFunc` seam does not receive the negotiated client capabilities,
  so the resource-read `_meta.ui` is host-independent. This is correct: a
  non-Apps host simply ignores `_meta.ui`; graceful degradation needs no
  per-host branching (RFC §7.5).
- Additive `Meta` fields on `runtime/server` types are non-breaking — every
  call site uses named-field struct literals.

## Glossary additions

- **MCP Apps extension** — `io.modelcontextprotocol/ui`, SEP-1865.
- **`_meta.ui`** — the nested Apps metadata object on a tool definition and on a
  resource-read response.
- **Deny-by-default CSP** — the Content-Security-Policy a UI resource gets when
  it declares no domains.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
