# Brief 03 — Official Go MCP SDK audit

**Date:** 2026-05-20
**Sources:** see Section 7 (all reachable via WebFetch/WebSearch on 2026-05-20 unless noted)
**Status:** Draft for RFC-001-Dockyard

## 1. Why this brief exists

Dockyard's MCP server core is a settled decision: it **builds on `github.com/modelcontextprotocol/go-sdk`** rather than re-implementing the protocol. That makes the SDK a load-bearing dependency. Before phase planning we need a precise read on three questions:

1. **Is the SDK safe to depend on?** — maturity, versioning policy, Go floor, CGo status, release cadence.
2. **How much does Dockyard get for free?** — server-side primitives (tools, resources, prompts, completion), transports, concurrency.
3. **Can Dockyard layer the Apps and Tasks extensions on top without forking?** — extension/`_meta`/capability hooks, and how cleanly a new experimental extension attaches.

This brief audits the SDK against those questions and delivers a free-vs-build verdict plus dependency risk. It does not cover the Apps or Tasks *protocol* surface itself (that belongs in the Apps/Tasks briefs); it covers only what the SDK gives Dockyard to implement them with.

## 2. Findings

### 2.1 Maturity, versioning, Go floor, CGo

- **Stable and 1.x.** The SDK reached **v1.0.0** (functionally equivalent to v0.8.0) which **formalized a compatibility guarantee: no breaking API changes going forward.** Current release is **v1.6.0 (2026-05-08)**; 24 releases total. It is maintained by the MCP project **in collaboration with Google** — not a community side-project.
- **Release cadence is brisk.** v1.0 → v1.6 spans roughly six months: v1.2.0, v1.3.0, v1.3.1, v1.4.0, v1.4.1, v1.5.0, v1.6.0. Cadence is a feature (fast spec tracking) and a risk (Dockyard must keep its dependency current — see Section 4).
- **Go floor: `go 1.25.0`** in `go.mod` on `main`. The floor moved up *within* the 1.x line (v1.4.1 raised it from 1.24 to 1.25). The README states releases "target only supported versions of Go" per the Go release policy. Dockyard must adopt a matching or higher floor and accept that the SDK will bump it on a rolling basis.
- **CGo status: effectively CGo-free.** Direct dependencies are all pure-Go (`golang-jwt/jwt/v5`, `google/go-cmp`, `google/jsonschema-go`, `segmentio/encoding`, `yosida95/uritemplate/v3`, `golang.org/x/oauth2`, `golang.org/x/time`, `golang.org/x/tools`; indirects `segmentio/asm`, `golang.org/x/sys`). None require CGo. This is consistent with Dockyard's no-CGo / single static binary constraint. (Caveat: not 100% audited transitively — flagged as Q-6.)
- **Spec coverage.** As of v1.0 the maintainers stated you can implement "essentially any behavior described in the MCP spec" with the SDK (the one historical exception, client-side OAuth, was stabilized in v1.5.0 — and client OAuth is out of Dockyard's server-only scope anyway). v1.2.0 added partial support for the **2025-11-25 spec**; the team is tracking toward the **2026-06-30 spec version**.

### 2.2 Server-side capabilities

The SDK fully covers the server primitives Dockyard needs. From package `github.com/modelcontextprotocol/go-sdk/mcp`:

- **Tools** — `func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])`. Generic over typed input/output; the SDK **infers JSON Schema from the Go types**, validates incoming arguments against the input schema, and unmarshals into the typed `In`. There is also a non-generic `(*Server).AddTool(t *Tool, h ToolHandler)` for dynamic cases.
- **Resources** — `(*Server).AddResource(r *Resource, h ResourceHandler)` and `(*Server).AddResourceTemplate(t *ResourceTemplate, h ResourceHandler)`. Resource templates use `yosida95/uritemplate/v3` — relevant because the Apps extension's `ui://` scheme can be served as ordinary resources.
- **Prompts** — `(*Server).AddPrompt(p *Prompt, h PromptHandler)`.
- **Completion** — `CompleteParams`, `CompleteResult`, `CompleteReference`, `CompletionCapabilities`, `CompletionResultDetails` types are present; argument completion is supported.
- **Other primitives** — elicitation (incl. URL-mode elicitation, SEP-1036; `URLElicitationRequiredError`), sampling incl. **sampling-with-tools** (`CreateMessageWithTools`, v1.4.0), logging, progress notifications, pagination (`ServerOptions.PageSize`, default 1000), icons/metadata (SEP-973).
- **Lifecycle hooks** — `ServerOptions.InitializedHandler`, `RootsListChangedHandler`; `KeepAlive` ping support exists (improved across releases).

Verdict: the full server-side primitive set is **free**. Dockyard writes zero protocol-primitive code.

### 2.3 Transports

The `mcp` package ships every transport Dockyard's three deployment modes (local stdio, remote HTTP, Portico-managed) require:

- `StdioTransport`, `CommandTransport`, `IOTransport`, `InMemoryTransport` (`NewInMemoryTransports()` — useful for the Dockyard inspector and contract tests), `LoggingTransport`.
- `StreamableServerTransport` / `StreamableClientTransport` — **streamable-HTTP**, the current MCP HTTP transport.
- `SSEServerTransport` / `SSEClientTransport` — legacy **SSE**, still present for backward compat.
- HTTP entry points: `NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts)` and `NewSSEHandler(...)`, both returning `http.Handler`. The `getServer` callback (server-per-request) is a natural seam for Dockyard's multi-tenant / per-session wiring.
- Security knobs: DNS-rebinding protection for localhost (default on since v1.4.0), Origin/Content-Type verification (v1.5.0), cross-origin protection (default **off** again as of v1.6.0 — Dockyard should re-enable explicitly for HTTP deployments).

Verdict: transports are **free**, including the `InMemoryTransport` Dockyard's inspector wants. Dockyard must own one thing the SDK does not: the **`postMessage`/iframe bridge** to the embedded UI. That bridge is not a transport in the SDK sense — it is the Apps extension's UI-to-host channel and lives on the frontend side plus a thin server contract.

### 2.4 Extension hooks — Apps and Tasks

This is the decisive area for Dockyard. The picture as of 2026-05-20:

- **Generic extension capability negotiation exists.** v1.4.0 added an `Extensions` field to capabilities (**SEP-2133**). The API is `(*ServerCapabilities).AddExtension(name string, settings map[string]any)` and the symmetric `(*ClientCapabilities).AddExtension(...)`. This is exactly the hook the Apps spec needs — Apps is negotiated as `extensions["io.modelcontextprotocol/ui"]` with `mimeTypes: ["text/html;profile=mcp-app"]`.
  - Historical gap: issue **#777** ("Extension declaration for MCP Apps") reported that `ClientCapabilities` originally lacked an extensions field. It was **closed via PR #794** — extension declaration now works on both client and server capabilities. (Server-side is what Dockyard needs.)
- **`_meta` is first-class everywhere.** `type Meta map[string]any` with `GetMeta()`/`SetMeta()`, surfaced as the `Meta` field (`json:"_meta,omitempty"`) on `Tool`, `CallToolParams`, `CallToolResult`, `Resource`, and other request/result types. The Apps extension links a tool to its `ui://` resource purely through `_meta`, so Dockyard can attach `_meta.ui.*` (or whatever the current Apps spec key is) **without any SDK change**.
- **Native Apps support: deliberately not shipped, by design.** Issue **#933** (MCP Apps interactive UIs, referencing SEP-1865) was **closed on 2026-05-05** with the maintainer conclusion that **"all primitives in place"** and that first-class Apps support is **"outside the scope of the 2026-06-30 version implementation."** Translation: the SDK gives you resources + `_meta` + extension capabilities and expects frameworks like Dockyard to compose Apps on top. This is good news for Dockyard's positioning — the Apps layer is *exactly* Dockyard's value-add — but it means **the Apps ergonomics are 100% Dockyard's to build**.
- **Tasks: experimental, in flight, not yet in a release.** Issue **#626** ("SEP-1686 (experimental): Implement Tasks") is **open, labeled "ready for work."** A related issue **#942** ("Tasks Extension implementation") was **closed on 2026-05-06**. As of v1.6.0 there is no documented stable `Tasks` API in the `mcp` package. The Tasks extension also has its own experimental repo (`modelcontextprotocol/experimental-ext-tasks`). For Dockyard V1, Tasks must be assumed **not free** — Dockyard layers it on the same extension + `_meta` primitives, or vendors the experimental module.
- **Custom JSON-RPC methods / notifications.** Extensions sometimes need non-standard methods or notifications. `AddSendingMiddleware`/`AddReceivingMiddleware` exist on both `Server` and `Client` and intercept the request pipeline (`m1(m2(m3(handler)))`). But issue **#745** ("Expose generic `SendNotification` on `ServerSession`", open, "needs investigation") shows a real gap: **there is no clean public API today to emit an arbitrary custom-method notification from a server session.** If the Apps or Tasks bridge needs a non-standard server→client notification, Dockyard may hit this wall. Tracked as Q-2.

### 2.5 API ergonomics & concurrency

- **Ergonomics are good and Go-idiomatic.** `AddTool[In, Out]` with schema inference from struct tags is close to the contract-first DX the braindump wants — Dockyard's `app.Tool(...).Input[T]().Output[T]()` builder can sit directly on top of it. `NewServer(impl *Implementation, opts *ServerOptions)` is a clean constructor. Handlers receive `context.Context` first (idiomatic) and a typed request.
- **Request context.** `ServerRequest[P]` exposes `GetExtra() *RequestExtra`, `GetParams()`, `GetSession() Session`. The `Session` interface lets a handler call back to the peer (`NotifyProgress`, etc.) — useful for Tasks progress and Apps UI updates.
- **Concurrency model.** The SDK is connection/session-oriented: `(*Server).Connect(ctx, transport, opts) (*ServerSession, error)` or `(*Server).Run(ctx, transport)`. One `Server` can serve many sessions (the HTTP handlers' `getServer` callback is per-request). Middleware applied right-to-left. The SDK "hides JSON-RPC details when not relevant to business logic." Per-session concurrency, in-flight request handling, and context cancellation are SDK-managed; Dockyard handler code must still be goroutine-safe for shared state. Detailed concurrency guarantees were not exhaustively documented in the sources consulted — flagged as Q-5.

## 3. Go-flavored shapes / API sketches

Real SDK surface Dockyard builds against (signatures as published on pkg.go.dev / GitHub, 2026-05-20):

```go
// Construction
func NewServer(impl *Implementation, options *ServerOptions) *Server
func (s *Server) Run(ctx context.Context, t Transport) error
func (s *Server) Connect(ctx context.Context, t Transport, opts *ServerSessionOptions) (*ServerSession, error)

// Typed tool registration — Dockyard's contract-first builder wraps this.
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])
type ToolHandlerFor[In, Out any] func(context.Context, *CallToolRequest, In) (*CallToolResult, Out, error)

// Resources — ui:// App resources register here.
func (s *Server) AddResource(r *Resource, h ResourceHandler)
func (s *Server) AddResourceTemplate(t *ResourceTemplate, h ResourceHandler)

// Extension capability negotiation — Apps/Tasks attach here.
func (c *ServerCapabilities) AddExtension(name string, settings map[string]any)

// _meta — tool→UI linkage rides on this; no SDK change needed.
type Meta map[string]any            // field: `_meta,omitempty` on Tool/Resource/Call*Params/Call*Result

// HTTP transports — Dockyard's `--transport http` and Portico mode.
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler
```

Sketch of how Dockyard layers the Apps extension *without forking the SDK*:

```go
// 1. Declare the Apps extension at server construction.
caps := &mcp.ServerCapabilities{}
caps.AddExtension("io.modelcontextprotocol/ui", map[string]any{
    "mimeTypes": []string{"text/html;profile=mcp-app"},
})
srv := mcp.NewServer(impl, &mcp.ServerOptions{Capabilities: caps})

// 2. Serve the UI bundle as an ordinary resource under ui://.
srv.AddResource(&mcp.Resource{URI: "ui://customer-health/main", MIMEType: "text/html;profile=mcp-app"}, serveEmbeddedHTML)

// 3. Link tool -> UI resource purely through _meta (Dockyard's manifest generates this).
tool := &mcp.Tool{Name: "show_customer_health", Meta: mcp.Meta{
    "io.modelcontextprotocol/ui": map[string]any{"resourceUri": "ui://customer-health/main"},
}}
mcp.AddTool(srv, tool, handleCustomerHealth)
```

Nothing above requires patching the SDK. The Tasks extension follows the same pattern (`AddExtension` + `_meta` task descriptors) until the SDK ships a native Tasks API — at which point Dockyard swaps its shim for the official type.

## 4. Sharp edges & risks

- **R1 — Tasks is not free for V1.** Tasks is V1 scope for Dockyard but the SDK has no released Tasks API (#626 open). Dockyard must build a Tasks shim on `_meta`/extension primitives and/or vendor `experimental-ext-tasks`, then migrate when the SDK lands native support. Migration churn is likely.
- **R2 — No clean custom-notification API.** #745 (open) means a server session cannot easily emit an arbitrary non-standard notification. If the Apps UI bridge or Tasks progress needs a custom server→client notification beyond standard `progress`, Dockyard may need a workaround (middleware, or its own jsonrpc handling) until #745 resolves.
- **R3 — Rolling Go floor.** The SDK raised its Go floor mid-1.x (1.24→1.25). Dockyard inherits a moving minimum; CI must track it and the project cannot pin an old Go toolchain.
- **R4 — Fast release cadence + security-driven changes.** Releases ship security-relevant default changes (case-sensitive JSON parsing; cross-origin protection toggled on in v1.4.1, off again in v1.6.0). Dockyard must (a) stay current and (b) **explicitly set security-relevant options** rather than trusting defaults, since defaults have flipped between releases.
- **R5 — Apps ergonomics are entirely Dockyard's.** The SDK maintainers explicitly scoped first-class Apps support *out* (#933). The SDK gives primitives only. This is opportunity, not bug — but it means the Apps DX, manifest wiring, `ui://` correctness, MIME handling, and host-compat all sit in Dockyard, with no SDK safety net.
- **R6 — Spec-version skew.** The SDK tracks toward the 2026-06-30 spec; the Apps spec (announced 2026-01-26) and Tasks SEP-1686 evolve on their own cadence. Dockyard's forward-compatibility requirement means isolating extension code behind an internal interface so a spec bump is a localized change.
- **R7 — `_meta` is untyped (`map[string]any`).** Convenient for layering extensions, but offers no compile-time safety. Dockyard should wrap `_meta` access in typed helpers so App/Task metadata bugs surface in Dockyard's own validation, not at runtime in a host.

## 5. What Dockyard must adopt / build / avoid

**Adopt (free from the SDK — do not reinvent):**
- The entire server primitive set: tools (incl. generic `AddTool[In,Out]` with schema inference), resources, resource templates, prompts, completion, elicitation, sampling, logging, progress, pagination.
- All transports: stdio, streamable-HTTP, SSE, in-memory, command. Use `InMemoryTransport` as the backbone of the Dockyard inspector and contract tests.
- The `ServerCapabilities.AddExtension` hook and the `_meta` (`Meta`) plumbing — these are the official seams for Apps and Tasks.
- JSON Schema generation via `google/jsonschema-go` (already a dependency) — feeds Dockyard's contract-first TypeScript codegen.
- Session lifecycle, middleware pipeline, keepalive, security knobs.

**Build (Dockyard's value-add — not in the SDK):**
- The **MCP Apps layer**: `ui://` resource conventions, tool↔UI `_meta` linkage, `text/html;profile=mcp-app` MIME correctness, the iframe/`postMessage` host bridge, host-compatibility checks, CSP/resource policy. SDK gives primitives; Dockyard gives the App.
- The **MCP Tasks layer** for V1: a shim over `_meta`/extension primitives (optionally vendoring `experimental-ext-tasks`), with an internal interface so the official SDK Tasks API can be swapped in later.
- Typed `_meta` accessors / validation so extension metadata is checked by Dockyard's toolchain.
- The contract-first builder DX (`app.Tool(...).Input[T]().Output[T]().UI(...)`) wrapping `AddTool`.
- Everything above the protocol: manifest, templates, CLI, local preview/inspector, codegen, packaging, quality gates, observability.

**Avoid:**
- **Forking the SDK.** The extension + `_meta` hooks make a fork unnecessary for Apps and Tasks; a fork would forfeit the v1.x compatibility guarantee and security updates. Layer, do not fork.
- **Relying on SDK defaults for security.** Set cross-origin protection, Origin/Content-Type verification, and DNS-rebinding protection explicitly.
- **Pinning an old SDK version.** The Tasks API, custom-notification fix (#745), and spec updates all arrive in future releases; Dockyard must stay current.
- **Hard-coding extension wire formats** (e.g. the exact `_meta` key for Apps UI linkage) outside one internal module — isolate for forward-compat (R6).

**Verdict.** Dockyard gets the entire MCP *protocol core* for free from a genuinely stable, Google-co-maintained, CGo-free, v1.x SDK with a no-breaking-changes guarantee — tools, resources, prompts, completion, all transports, capability negotiation, and the `_meta`/extension hooks needed to layer Apps and Tasks **without forking**. What Dockyard must build is precisely its reason to exist: the Apps experience (the SDK explicitly scoped first-class Apps support out), a Tasks shim for V1 (no released SDK Tasks API yet), and the entire framework above the protocol. Dependency risk is **low-to-moderate**: low on stability and licensing, moderate on (a) keeping pace with a fast release cadence and rising Go floor, and (b) the missing custom-notification API (#745) which could pinch the Apps/Tasks bridge. The SDK is the right foundation; depend on it, pin a recent version, and isolate all extension code behind a Dockyard-owned interface.

## 6. Open questions (feed RFC open-questions section)

- **Q-1.** When will the SDK ship a *released, stable* Tasks API (tracking #626)? Does Dockyard V1 build a `_meta` shim now and migrate, or wait/vendor `experimental-ext-tasks`?
- **Q-2.** Can the Apps UI bridge and Tasks progress be expressed with the SDK's standard notifications, or do they need custom server→client notifications blocked by #745? If blocked, what is the interim workaround?
- **Q-3.** What exact extension identifier and `_meta` key shape does the *current* Apps spec mandate (`io.modelcontextprotocol/ui` + `resourceUri`?), and is it stable enough to encode in Dockyard's manifest generator?
- **Q-4.** Which MCP spec version does Dockyard V1 target — 2025-11-25 (SDK v1.2+ partial) or 2026-06-30 (SDK in-progress) — and how does that gate the SDK version floor?
- **Q-5.** What are the SDK's documented per-session concurrency guarantees (in-flight request ordering, cancellation, goroutine safety of `ServerSession`)? Needs confirmation from `docs/` or source before Dockyard relies on shared-state patterns.
- **Q-6.** Has the SDK's full transitive dependency graph been verified CGo-free and license-compatible (Dockyard ships static binaries)?
- **Q-7.** Should Dockyard pin a minimum SDK version and run a compatibility-test matrix against new SDK releases in CI, given the security-default flips between v1.4.1/v1.5.0/v1.6.0?

## 7. Sources

All consulted on 2026-05-20; all reachable unless noted.

- `https://github.com/modelcontextprotocol/go-sdk` — repo README, examples, package overview.
- `https://github.com/modelcontextprotocol/go-sdk/releases` — release notes v1.0.0–v1.6.0.
- `https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.0.0` — v1.0 stability/compatibility guarantee.
- `https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp` — `mcp` package API surface (types, signatures).
- `https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#ServerOptions` — `ServerOptions`, `ServerCapabilities.AddExtension`, `Tool`/`Meta`/`CallTool*` structs, `ToolHandlerFor`.
- `https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/main/go.mod` — Go floor (`go 1.25.0`) and dependency list.
- `https://github.com/modelcontextprotocol/go-sdk/issues/777` — Extension declaration for MCP Apps (closed via PR #794).
- `https://github.com/modelcontextprotocol/go-sdk/issues/933` — MCP Apps interactive UIs (closed 2026-05-05; "all primitives in place", first-class Apps out of scope).
- `https://github.com/modelcontextprotocol/go-sdk/issues/626` — SEP-1686 Tasks (open, "ready for work").
- `https://github.com/modelcontextprotocol/go-sdk/issues?q=...` — issue scan: #745 (custom notification), #735, #657; closed #942 (Tasks), #933, #628/#627 (auth).
- `https://blog.modelcontextprotocol.io/posts/2026-01-26-mcp-apps/` and `https://github.com/modelcontextprotocol/ext-apps` — MCP Apps extension context (extension id `io.modelcontextprotocol/ui`, `ui://`, `text/html;profile=mcp-app`).
- WebSearch (2026-05-20) — corroborating SDK version/status, middleware semantics, Tasks experimental repo.

Note: the SDK `docs/` directory and `CONTRIBUTING.md` were referenced but not fetched in full; detailed concurrency guarantees (Q-5) and a full transitive-dependency audit (Q-6) remain to be confirmed directly from source.
