# Brief 01 — MCP Apps extension

**Date:** 2026-05-20
**Sources:** (full URLs in §7; reachability noted inline)

- `https://modelcontextprotocol.io/extensions/apps/overview` — reachable, fetched.
- `https://apps.extensions.modelcontextprotocol.io/api/` — reachable, but it is a landing/index page only; no API surface extractable directly.
- `https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx` (and `raw.githubusercontent.com` mirror) — reachable, fetched; this is the authoritative spec (SEP-1865, dated 2026-01-26).
- `https://modelcontextprotocol.io/extensions/apps/build` — reachable, fetched (server-side build guide).
- `https://apps.extensions.modelcontextprotocol.io/api/documents/Patterns.html` — reachable, fetched (server-side CSP/CORS patterns).
- `https://modelcontextprotocol.io/specification` capability mechanism — covered via the apps spec's capability-negotiation section; the core spec page was not separately fetched, so the `extensions` capability block is reported as the apps spec presents it.

**Status:** Draft for RFC-001-Dockyard

---

## 1. Why this brief exists

MCP Apps is the core protocol Dockyard implements. Every Dockyard product
promise — typed contracts, paved-road templates, quality gates, host
compatibility checks, packaging — sits on top of a single protocol surface:
a server that exposes `ui://` resources, links tools to them via `_meta`,
serves a specific HTML MIME type, and speaks a `postMessage` JSON-RPC dialect
to a sandboxed iframe. This brief pins down that surface precisely so RFC-001
can scope what Dockyard must implement **server-side** to be fully
Apps-compliant, and so the framework can encode forward-compatibility against
a spec that is explicitly "under active development."

The MCP Apps extension was formalized as **SEP-1865**, with a stable spec
revision dated **2026-01-26**, evolved from the community `mcp-ui` project and
co-developed by MCP-UI maintainers with OpenAI and Anthropic. The extension
identifier is `io.modelcontextprotocol/ui`.

---

## 2. Findings

### 2.1 What an MCP App is at the protocol level

An MCP App is the combination of **two existing MCP primitives plus metadata**:

1. A **tool** whose definition carries `_meta.ui.resourceUri` pointing at a
   UI resource.
2. A **resource** whose URI uses the `ui://` scheme and whose `mimeType` is
   `text/html;profile=mcp-app`, returning an HTML document (typically a
   single bundled file).

There is no new "app" primitive on the wire. An App is a *convention layered
on tools + resources*, made discoverable through `_meta` and made optional
through capability negotiation. The host fetches the `ui://` resource, renders
its HTML inside a sandboxed iframe, and bridges a JSON-RPC channel between that
iframe ("the View") and the host over `window.postMessage`.

### 2.2 The `ui://` resource and its MIME type

- **URI scheme:** resources must start with `ui://` (e.g.
  `ui://get-time/mcp-app.html`, `ui://weather-server/dashboard`). The path
  after `ui://` is arbitrary and server-chosen.
- **MIME type:** `text/html;profile=mcp-app` — the *only* type supported in
  the current MVP. The `ext-apps` SDK exports this as `RESOURCE_MIME_TYPE`.
- **Resource shape:** `{ uri, name, description?, mimeType, _meta?.ui? }`.
- **`resources/read` response:** `contents[]` with each entry carrying
  `{ uri, mimeType, text? | blob?, _meta?.ui? }`. HTML is delivered as `text`
  (string) or `blob` (base64).
- **Important:** `_meta.ui.csp` and `_meta.ui.domain` are read from the
  `contents[]` entry returned by the resource-read callback — *not* only from
  the static resource declaration. Dockyard must set CSP/domain on the
  resource-read response.

### 2.3 How tools link to UI resources via `_meta`

A tool declares its UI through nested metadata:

```text
_meta.ui = {
  resourceUri?: string,                       // a ui:// URI
  visibility?: Array<"model" | "app">         // default ["model","app"]
}
```

- `resourceUri` lets the host **preload** the UI before the tool is even
  called (enabling streaming tool inputs into the App).
- `visibility` controls who can invoke the tool: `"model"` makes it callable
  by the agent/LLM; `"app"` restricts it to same-server App-initiated calls.
  Omitting it defaults to both. `visibility: ["app"]` is the standard pattern
  for UI-only actions (cart updates, polling) that should not pollute the
  model's tool list.
- A **deprecated flat form** exists: `_meta["ui/resourceUri"]`. Dockyard should
  emit only the nested form but tolerate reading the flat form.

### 2.4 The postMessage / sandboxed-iframe bridge

The View and host communicate via a **JSON-RPC dialect of MCP transported over
`postMessage`** (not stdio/HTTP). Some methods are shared with core MCP
(`tools/call`), some are analogues (`ui/initialize`), most are new and
`ui/`-prefixed.

**Lifecycle:** the View sends `ui/initialize`:

```text
{ "jsonrpc":"2.0", "id":N, "method":"ui/initialize",
  "params": { "protocolVersion": string,
              "capabilities": { "appCapabilities"?: McpUiAppCapabilities },
              "clientInfo": { "name": string, "version": string } } }
```

The host replies with `McpUiInitializeResult` carrying `hostContext`,
`hostCapabilities`, and `hostInfo`. The View must wait for
`ui/notifications/initialized` before assuming readiness.

`hostContext` fields include: `toolInfo`, `theme`, `styles`, `displayMode`,
`availableDisplayModes`, `containerDimensions`, `locale`, `timeZone`,
`userAgent`, `platform`, `deviceCapabilities`, `safeAreaInsets`.
`styles.variables` supplies standardized CSS custom properties
(`--color-background-primary`, `--font-sans`, `--border-radius-md`, etc.) so
Apps can theme themselves to the host.

**View → Host requests:**

- `ui/open-link` — `{ url: string }`
- `ui/message` — `{ role, content }` (send a message into the chat)
- `ui/request-display-mode` — `{ mode }` (`"inline"`, `"fullscreen"`, `"pip"`)
- `ui/update-model-context` — `{ content?, structuredContent? }`
- `tools/call` — proxied through the host to the server.

**Host → View notifications:**

- `ui/notifications/tool-input` — `{ arguments }` (sent before the result)
- `ui/notifications/tool-input-partial` — `{ arguments }` (streaming inputs)
- `ui/notifications/tool-result` — a standard MCP `CallToolResult`
- `ui/notifications/tool-cancelled` — `{ reason }`
- `ui/notifications/size-changed` — `{ width, height }`
- `ui/notifications/host-context-changed` — `Partial<HostContext>`
- `ui/notifications/sandbox-proxy-ready`,
  `ui/notifications/sandbox-resource-ready` — `{ html, sandbox?, csp?, permissions? }` (reserved sandbox-proxy path).
- `ui/resource-teardown` — cleanup, bidirectional.

The host **proxies** all View-initiated `tools/call` to the MCP server. This
matters for Dockyard: a tool call from the App arrives at the server over the
*normal* MCP transport — there is no separate App-only server endpoint.

### 2.5 CSP and sandboxing

- The App always runs in a sandboxed `<iframe>`: no parent DOM access, no host
  cookies/localStorage, no parent navigation, no script execution in the
  parent context.
- **Default CSP** (applied when `_meta.ui.csp` is omitted) is deny-by-default:

  ```text
  default-src 'none'; script-src 'self' 'unsafe-inline';
  style-src 'self' 'unsafe-inline'; img-src 'self' data:;
  media-src 'self' data:; connect-src 'none';
  ```

  This is why single-file HTML bundles (e.g. `vite-plugin-singlefile`) are the
  path of least resistance — no external origins to declare.
- To allow network/assets, the resource declares `_meta.ui.csp` with:
  `connectDomains` (fetch/XHR/WebSocket), `resourceDomains` (scripts, styles,
  images, fonts), `frameDomains`, `baseUriDomains`.
- `_meta.ui.permissions` requests iframe permissions: `camera`, `microphone`,
  `geolocation`, `clipboardWrite` (each an object, presence = requested).
- `_meta.ui.domain` requests a **stable dedicated origin** for the iframe —
  needed for APIs that allowlist origins (CORS). Claude derives this origin as
  a SHA-256 hash of the MCP server URL: `<hash32>.claudemcpcontent.com`.
- **Hosts may further restrict but never loosen** these declarations. CSP is
  mandatory; a host cannot grant an undeclared domain.

### 2.6 Structured content / tool-result shape consumed by the UI

A tool result is a standard MCP `CallToolResult`:

```text
{
  content: ContentBlock[],       // text/image/etc — enters model context
  structuredContent?: object,    // typed data for the UI — NOT in model context
  _meta?: object                 // metadata, e.g. viewUUID for view-state
}
```

The key distinction: `content` is the model-facing representation;
`structuredContent` is the **UI-optimized payload** that the App renders and
which is *deliberately excluded from the model's reasoning context*. Dockyard's
contract-first design (typed Go output structs → JSON Schema → generated TS
types) maps directly onto `structuredContent`. `_meta.viewUUID` is the hook for
view-state persistence across re-renders.

### 2.7 Capability negotiation — Apps as an optional extension

Apps is negotiated through the core MCP **`extensions`** capability block. The
host advertises support during `initialize`:

```json
{ "capabilities": {
    "extensions": {
      "io.modelcontextprotocol/ui": { "mimeTypes": ["text/html;profile=mcp-app"] }
    } } }
```

If a connecting host does not advertise `io.modelcontextprotocol/ui`, the
server must behave as a plain MCP server: the same tools still work, but the
server should not assume the UI will render. This is the central
forward-/backward-compatibility lever and the mechanism Dockyard relies on for
graceful degradation.

### 2.8 How host support and behavior diverge

Apps is explicitly an extension; host support varies. As of early 2026:

- **Supporting hosts:** Claude / Claude Desktop, ChatGPT, VS Code GitHub
  Copilot, Goose, Postman, MCPJam. (See the MCP "client matrix.")
- **Known divergences:**
  - **Claude** requires domain signing — the `<hash>.claudemcpcontent.com`
    subdomain derived from the server URL.
  - **VS Code** does not support `fullscreen` or `pip` display modes.
  - **ChatGPT** does not support every capability — notably no tool-calling
    from the UI in some configurations, and no host-checkout/file helpers.
  - **Non-supporting hosts** (e.g. Dify at time of writing) degrade an App to
    escaped HTML markup in a thoughts panel rather than rendering it.

The practical conclusion: Dockyard's "host compatibility checks" are a real,
load-bearing feature, not a nicety. The framework must model a per-host
capability matrix and warn when an App uses features (`pip`, UI-initiated
`tools/call`, specific permissions) unsupported by a declared target host.

---

## 3. Go-flavored shapes / API sketches

These are sketches for RFC discussion, not final API. They build on
`github.com/modelcontextprotocol/go-sdk` for the MCP server core; the Apps
extension types below are what Dockyard would add on top.

```go
// MIME constant — the single supported App resource type.
const ResourceMIMEType = "text/html;profile=mcp-app"

// ExtensionID is the capability key for Apps.
const ExtensionID = "io.modelcontextprotocol/ui"

// UIMeta is the `_meta.ui` object shared by tool and resource metadata.
type UIMeta struct {
    ResourceURI   string       `json:"resourceUri,omitempty"`   // tool side
    Visibility    []string     `json:"visibility,omitempty"`    // "model" | "app"
    CSP           *CSPPolicy   `json:"csp,omitempty"`           // resource side
    Permissions   *Permissions `json:"permissions,omitempty"`   // resource side
    Domain        string       `json:"domain,omitempty"`        // dedicated origin
    PrefersBorder *bool        `json:"prefersBorder,omitempty"`
}

type CSPPolicy struct {
    ConnectDomains  []string `json:"connectDomains,omitempty"`
    ResourceDomains []string `json:"resourceDomains,omitempty"`
    FrameDomains    []string `json:"frameDomains,omitempty"`
    BaseURIDomains  []string `json:"baseUriDomains,omitempty"`
}

// Permissions: presence of a pointer == requested.
type Permissions struct {
    Camera         *struct{} `json:"camera,omitempty"`
    Microphone     *struct{} `json:"microphone,omitempty"`
    Geolocation    *struct{} `json:"geolocation,omitempty"`
    ClipboardWrite *struct{} `json:"clipboardWrite,omitempty"`
}
```

Contract-first registration (the DX the braindump describes):

```go
// In ≈ tool input struct, Out ≈ tool output struct.
// Out is serialized into CallToolResult.structuredContent.
dockyard.App[ShowCustomerHealthIn, ShowCustomerHealthOut]{
    Tool:        "show_customer_health",
    Description: "Show an interactive customer health dashboard for an account.",
    UIResource:  "ui://customer-health/main",     // -> _meta.ui.resourceUri
    Visibility:  []string{"model", "app"},
    Handler:     handleShowCustomerHealth,        // returns ShowCustomerHealthOut
}
```

A capability-aware result writer (forward-compat seam):

```go
// Result is what a Dockyard tool handler returns; Dockyard maps it to
// CallToolResult, splitting model-facing text from UI-facing structured data.
type Result[Out any] struct {
    Text       string // -> content[]{type:"text"}, enters model context
    Structured Out    // -> structuredContent, UI-only
    Meta       map[string]any
}

// HostCaps captured from the initialize handshake; nil ext => plain MCP host.
type HostCaps struct {
    AppsSupported bool
    MIMETypes     []string
}
```

The `ui://` resource handler returns CSP/domain on the read response itself:

```go
func (a *appResource) Read(ctx context.Context) ResourceContents {
    return ResourceContents{
        URI:      "ui://customer-health/main",
        MIMEType: ResourceMIMEType,
        Text:     embeddedHTML,                 // single-file bundle
        Meta: UIMetaWrap{UI: UIMeta{
            CSP:    &CSPPolicy{ConnectDomains: []string{"https://api.company.com"}},
            Domain: "customer-health",          // host derives signed origin
        }},
    }
}
```

The `postMessage` JSON-RPC dialect (`ui/initialize`, `ui/notifications/*`,
`ui/open-link`, etc.) is **client-side** — it runs in the Svelte iframe, not
the Go server. Dockyard's only Go-side concern there is the **local inspector**
(the test-only host) which must implement the host half of that dialect to
render and drive Apps during `dockyard dev`.

---

## 4. Sharp edges & risks

1. **The bridge protocol is client-side; the inspector must implement the
   host half.** To preview Apps locally, Dockyard's inspector needs a working
   `ui/initialize` handshake, notification fan-out, sandboxed-iframe rendering,
   CSP enforcement, and `tools/call` proxying. This is non-trivial and is the
   one place Dockyard ships client-shaped code.

2. **CSP lives on the resource-read response, not the manifest.** A natural
   mistake is to model CSP only as static manifest config. Dockyard must thread
   `_meta.ui.csp` / `_meta.ui.domain` into every `resources/read` reply.

3. **Domain signing is host-specific.** Claude's
   `<sha256>.claudemcpcontent.com` derivation is a Claude implementation
   detail, not a spec mandate. Dockyard must not hardcode it; it should treat
   "dedicated origin" abstractly and apply host-specific derivations behind a
   pluggable host profile.

4. **Spec churn.** The spec is "under active development"; the stable revision
   is `2026-01-26` and there is a separate `draft` path. Reserved methods
   (`ui/notifications/sandbox-proxy-*`) signal in-flight design. The
   `_meta["ui/resourceUri"]` flat form is already deprecated. Forward-compat
   requires (a) versioned codecs keyed on the negotiated `protocolVersion`,
   (b) tolerant reads of deprecated shapes, (c) no assumptions beyond the
   advertised `mimeTypes`.

5. **Single MIME type today.** Only `text/html;profile=mcp-app` is supported.
   Dockyard should treat the MIME type as a constant but route it through one
   choke point so future profile types are a one-line change.

6. **Host feature divergence is real and silent.** Using `pip`,
   UI-initiated `tools/call`, or permissions that a target host ignores yields
   a degraded experience with no error. Compatibility checks must be a build-
   time gate, not docs.

7. **`structuredContent` vs `content` discipline.** Putting large UI payloads
   in `content` pollutes (and inflates) model context. Dockyard's typed-output
   path must route UI data exclusively to `structuredContent`; the braindump's
   "oversized output payloads" warning maps directly here.

8. **Tool visibility footgun.** A UI-only action left at default visibility
   becomes model-callable. Dockyard should make `visibility: ["app"]` an
   explicit, lint-checked choice for action-style tools.

9. **Preloading implies UI before result.** Hosts may fetch and render the
   `ui://` resource before the tool runs, then push `tool-input` /
   `tool-result` later. Generated UIs must render a loading state correctly —
   reinforcing the braindump's mandatory loading/empty/error states.

---

## 5. What Dockyard must adopt / build / avoid

**Must adopt (server-side compliance baseline):**

- Register `ui://` resources with `mimeType = text/html;profile=mcp-app`.
- Serve resource HTML via `resources/read` as `text` or `blob`, including
  `_meta.ui` (`csp`, `permissions`, `domain`, `prefersBorder`) on the response.
- Emit tool definitions carrying `_meta.ui.resourceUri` (nested form) and
  `_meta.ui.visibility`.
- Advertise / negotiate the `io.modelcontextprotocol/ui` extension capability
  with `mimeTypes: ["text/html;profile=mcp-app"]`; behave as a plain MCP server
  when a host does not advertise it.
- Return tool results as `CallToolResult` with model-facing text in `content`
  and UI data in `structuredContent`; support `_meta` (incl. `viewUUID`).
- Accept App-proxied `tools/call` over the normal MCP transport (no special
  endpoint).

**Must build (the paved road on top of compliance):**

- Contract-first codegen: Go input/output structs → JSON Schema → generated
  Svelte/TS types, with `structuredContent` as the typed UI payload.
- A choke-pointed CSP/domain layer so resource-read responses always carry
  correct `_meta.ui`; default to single-file bundles (deny-by-default CSP).
- A local **inspector** that implements the *host half* of the `ui/`
  postMessage dialect: handshake, notifications, sandboxed iframe, CSP
  enforcement, `tools/call` proxy, fixture injection.
- A per-host **capability matrix** + build-time compatibility checks (display
  modes, UI tool-calling, permissions, domain signing) feeding `dockyard
  validate`.
- Versioned protocol codecs keyed on negotiated `protocolVersion`, plus
  tolerant reads of deprecated shapes (`_meta["ui/resourceUri"]`).
- Pluggable host profiles for host-specific derivations (e.g. Claude's signed
  `claudemcpcontent.com` origin).

**Must avoid:**

- Hardcoding host-specific behavior (Claude domain hashing) into the core.
- Treating CSP as static manifest-only config.
- Emitting the deprecated flat `_meta["ui/resourceUri"]`.
- Assuming Apps support — never skip plain-MCP behavior.
- Letting UI payloads leak into `content`.
- Defaulting action tools to model visibility.
- Implementing a bespoke App-server channel — Apps reuse tools + resources.

---

## 6. Open questions (feed RFC open-questions section)

- **Q-1.** Does Dockyard depend on the `@modelcontextprotocol/ext-apps`
  *server* helpers conceptually only (re-implementing in Go), or mirror their
  exact registration API? The Go SDK has no Apps module yet — confirm Dockyard
  owns the Go-side Apps layer entirely.
- **Q-2.** Inspector scope: does V1's inspector implement the full host half of
  the `ui/` dialect (all notifications, all display modes, sandbox-proxy path),
  or a pragmatic subset? Where is the line for V1?
- **Q-3.** How does Dockyard model host-specific quirks — a static built-in
  host-profile registry, or user-extensible profiles? Who maintains the
  capability matrix as hosts evolve?
- **Q-4.** Forward-compat strategy: pin to spec revision `2026-01-26` and gate
  upgrades, or track `draft`? How are negotiated `protocolVersion` values
  mapped to codecs?
- **Q-5.** Does Dockyard auto-derive `_meta.ui.domain` (and host-specific
  signed origins), or require the developer to declare it in the manifest?
- **Q-6.** Should `dockyard validate` treat `connectDomains`/`resourceDomains`
  usage without a matching declared target host as a warning or a build
  blocker?
- **Q-7.** Single-file bundling vs multi-asset: is `vite-plugin-singlefile`
  (deny-by-default CSP, zero external origins) the enforced default for
  generated apps, with multi-asset/CSP an opt-out?
- **Q-8.** How are reserved/unstable methods (`ui/notifications/sandbox-proxy-*`)
  surfaced — ignored, or exposed behind an experimental flag?
- **Q-9.** Where does `_meta.viewUUID`-based view-state persistence live —
  framework-managed, or left to the app author?

---

## 7. Sources

- MCP Apps — overview: https://modelcontextprotocol.io/extensions/apps/overview (reachable)
- MCP Apps — build guide (server-side): https://modelcontextprotocol.io/extensions/apps/build (reachable)
- MCP Apps — API landing page (index only): https://apps.extensions.modelcontextprotocol.io/api/ (reachable; no API surface inline)
- MCP Apps — server-side patterns (CSP/CORS): https://apps.extensions.modelcontextprotocol.io/api/documents/Patterns.html (reachable)
- MCP Apps specification, revision 2026-01-26 (SEP-1865): https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx (reachable)
- ext-apps repository: https://github.com/modelcontextprotocol/ext-apps (reachable)
- SEP-1865 PR: https://github.com/modelcontextprotocol/modelcontextprotocol/pull/1865 (reachable)
- MCP blog — "MCP Apps - Bringing UI Capabilities to MCP Clients" (2026-01-26): https://blog.modelcontextprotocol.io/posts/2026-01-26-mcp-apps/ (reachable)
- Alpic AI — "MCP Apps Goes Official: Claude (and more!) support" (host divergence detail): https://alpic.ai/blog/mcp-apps-goes-official-claude-chatgpt-support (reachable)
- The Register — "Claude supports MCP Apps" (2026-01-26): https://www.theregister.com/2026/01/26/claude_mcp_apps_arrives (reachable)
