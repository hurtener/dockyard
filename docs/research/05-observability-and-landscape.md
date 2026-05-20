# Brief 05 — Observability & competitive landscape

**Date:** 2026-05-20
**Sources:** See §7. mcp-mesh.ai homepage reachable; mcp-mesh.ai/pricing returned HTTP 404 (no public pricing page exists — noted below). All other URLs reachable as of 2026-05-20.
**Status:** Draft for RFC-001-Dockyard

## 1. Why this brief exists

Dockyard's mission is the best **open-source** framework for **secure, observable, scalable** MCP Servers and MCP Apps. Two of those three adjectives — *observable* and the implied debug DX — are decided by tooling, not protocol plumbing. This brief surveys the 2026 landscape of MCP server observability and debugging so RFC-001 can settle three things:

1. What "observability as a protocol" concretely means for an MCP Server — which events to emit and the canonical contract shape — so the V1 inspector and the post-V1 multi-server console are both pure protocol clients (the Harbor Console model).
2. What the V1 local single-server inspector must surface to make Dockyard's debug DX beat mcp-use, MCPJam, and the official MCP Inspector.
3. Where Dockyard's genuinely-OSS positioning wins against the cloud-funnel pattern that mcp-mesh.ai represents.

The braindump (Dump 4) is explicit: the inspector is a *client side for testing only*, observability mirrors the Harbor Console (runtime is headless, emits a canonical event/state contract, console is a pure protocol client), and the multi-server fleet console is post-V1.

## 2. Findings

### 2.1 mcp-mesh.ai — what it is and how the funnel works

MCP Mesh (mcp-mesh.ai, repo `dhyansraj/mcp-mesh`, v2.2.0 March 2026) is **not** an MCP App framework and **not** a direct Dockyard competitor. It is a Kubernetes-native distributed *service mesh / agent runtime* for Python/Java/TypeScript agents, built around "Distributed Dynamic Dependency Injection" — agents discover and inject each other at runtime across machines via a shared Rust FFI core, bridging MCP, Google A2A v1.0, and REST.

The codebase is genuinely MIT-licensed and self-hostable; there is **no hosted SaaS tier and no public pricing page** (the `/pricing` URL 404s — the funnel is *not* a paywall). The cloud-funnel pattern here is subtler and is the real anti-pattern to study:

- **Observability is delegated, not owned.** "Built-in observability" means *generated Grafana dashboards + Tempo distributed tracing + Redis-backed sessions* — i.e. the project ships Helm charts that wire your data into a third-party observability stack. There is no native, self-contained, protocol-level observability surface. To actually *see* anything you must stand up and operate Grafana/Tempo/Redis, or point them at a managed Grafana/Tempo cloud.
- **`meshctl` is the on-ramp, the stack is the lock-in.** A single CLI carries you "from first agent through production"; observability, tracing, and registry inspection are all `meshctl` subcommands — but they are thin clients over heavy infrastructure (a registry service, Tempo, Grafana) that an enterprise will not self-operate at scale, creating a natural pull toward a managed cluster / vendor-hosted control plane.

**The anti-pattern for Dockyard to avoid:** *observability that only works if you adopt and operate an external stack.* MCP Mesh's observability is not a property of the runtime; it is an integration you assemble. A developer with one MCP server and ten minutes gets nothing without first deploying Grafana. Dockyard must invert this: observability is an intrinsic, zero-dependency property of every Dockyard server, surfaced by a built-in inspector, with OTel export as an *option* — not a prerequisite.

### 2.2 The 2026 MCP observability landscape

- **Vendor MCP servers, not MCP-server observability.** Datadog, Grafana, Sentry, Splunk, New Relic, Honeycomb, Dynatrace, PagerDuty, IBM Instana all shipped *MCP servers that expose their telemetry to agents*. This is the inverse of Dockyard's concern. The one genuinely relevant entrant is **Sentry's MCP server monitoring** (`wrapMcpServerWithSentry(McpServer)`): one-line wrap, JavaScript-only, captures tool usage by client/transport, per-tool latency and slowest tools, tool-call failures (including the "silent failures the MCP protocol normally masks"), resource access frequency, client segmentation, and transport distribution — down to individual JSON-RPC requests with arguments/results. It explicitly follows OpenTelemetry MCP semantic conventions.
- **OpenTelemetry MCP semantic conventions exist and are the canonical vocabulary.** As of Semantic Conventions 1.40.0 (April 2026) the MCP page is still labeled *Development* but is concrete (`opentelemetry.io/docs/specs/semconv/gen-ai/mcp/`). Two span kinds: `span.mcp.client` and `span.mcp.server`, both named `{mcp.method.name} {target}`, covering `tools/call`, `resources/read`, `resources/subscribe`, `prompts/get`, etc. Canonical attributes: required `mcp.method.name`; conditionally required `error.type`, `gen_ai.tool.name`, `gen_ai.prompt.name`, `jsonrpc.request.id`, `mcp.resource.uri`; recommended `mcp.protocol.version`, `mcp.session.id`, `network.transport`, `gen_ai.operation.name` (= `execute_tool` for tool calls); opt-in `gen_ai.tool.call.arguments` / `gen_ai.tool.call.result` (sensitive). MCP tool-call spans are designed to *merge into* a parent GenAI `execute_tool` span when an outer agent instrumentation is already tracing — relevant because a Harbor agent calling a Dockyard server is exactly that case.
- **The MCP protocol already has a logging capability.** Servers declare a `logging` capability and emit `notifications/message` with RFC 5424 severities, optional logger names, and arbitrary JSON data; clients call `logging/setLevel` to control verbosity. Default threshold is `warning`. This is a real, standardized, in-band channel — Dockyard's observability protocol should *extend* this idea rather than replace it, but the MCP logging capability alone is too thin (no latency, no structured tool-call lifecycle, no app/UI events).
- **stdio debugging is genuinely painful.** The dominant developer pain: on stdio transport, any write to stdout corrupts the JSON-RPC stream — `fmt.Println` in Go, `console.log` in Node, `print` in Python all break the server. Workarounds in the wild are crude: log to stderr, tail `/tmp/mcp-debug.log`, or run a logging proxy (mitmproxy / custom) between client and server to capture every byte. This is a direct DX opening: a Dockyard server that emits a structured observability stream *out-of-band* (not over the protocol stdio pipe) and renders it in an inspector eliminates the entire class of problem.

### 2.3 MCP inspectors / debuggers — the field

- **Official MCP Inspector** (`modelcontextprotocol/inspector`): React UI (MCPI) + a Node proxy (MCPP), `npx`-launched at `localhost:6274`. Shows the initialize handshake, `tools/list`, individual tool-call results, and a JSON-RPC message log. Baseline only; no MCP Apps/widget rendering, no latency analytics, no fixtures, no host-compat checks. (Note: CVE-2025-49596 — RCE in older versions; a cautionary tale for any inspector that proxies an untrusted server.)
- **mcp-use inspector** (`mcp-use/inspector`, MIT/open): the DX bar from the braindump. Modern inspector for remote MCP servers *with first-class MCP Apps / OpenAI Apps SDK support* — a Chrome-DevTools-style **widget emulator** that fully emulates the `window.openai` API, device switching (Desktop/Tablet/Mobile), locale change, CSP-permission testing, light/dark, hover/touch, safe-area insets, plus an RPC logger with method filtering and search, and widget state inspection.
- **MCPJam inspector** (`MCPJam/inspector`, Apache-2.0, ~2k stars): "Debug, Chat, Inspect, Evaluate" for MCP servers, MCP apps, and ChatGPT apps. JSON-RPC + OAuth trace visibility, OAuth conformance checks across spec versions, token-usage view, side-by-side model comparison, an App Builder with a widget emulator. **Open core**: free hosted web app + free desktop apps, but a paid tier exists at mcpjam.com/pricing.

**Reading the field:** the bar is no longer "see JSON-RPC messages." It is *render the MCP App, emulate the host bridge, switch devices/locales, test CSP, and trace OAuth* — all locally. Dockyard's inspector must clear that bar **and** add the things none of them have: per-tool latency analytics over time, a structured observability event stream as a first-class object, fixture-driven state testing, host-compatibility verdicts, and contract-drift detection — because Dockyard owns the server *and* its generated contracts.

### 2.4 MCP server frameworks (Go and otherwise)

- **Go:** the official `github.com/modelcontextprotocol/go-sdk` (with Google) — typed tool handlers, JSON-schema generation from structs, stdio + command transports. The community `mark3labs/mcp-go` tracks spec `2025-11-25` (back-compat to `2024-11-05`) and adds SSE + streamable-HTTP. Neither ships observability, an inspector, MCP Apps scaffolding, fixtures, or quality gates — they are protocol SDKs, not paved roads. **This is Dockyard's gap to own in Go.**
- **TypeScript/other:** `mcp-use` is the closest analog (scaffolding, CLI, hot reload, server + UI widgets, inspector) — the DX reference. None of the TS frameworks treat observability as a canonical protocol; logging is ad hoc and monitoring is "wrap with Sentry / export OTel."

### 2.5 MCP Apps observability surface (server-side, V1 scope)

MCP Apps (ext-apps spec `2026-01-26`) renders bundled HTML served via the `ui://` scheme inside a sandboxed iframe, tools declare `_meta.ui.resourceUri`, and the iframe talks to the host over JSON-RPC-on-`postMessage` — a `ui/*` dialect (`ui/initialize`, plus shared methods like `tools/call`). Host support varies (Claude, Claude Desktop, VS Code Copilot, Goose, Postman, MCPJam) and **host-compat bugs are real**: e.g. ext-apps issue #482 — Claude completes `resources/read` of a `ui://` URI but never mounts the iframe or starts the bridge. Dockyard's server can only see *its* half of this (resource served, bridge handshake received or not), and that visibility is exactly what makes host-compat issues debuggable. The observability protocol must therefore include app-load and bridge-lifecycle events, not just tool calls.

## 3. Go-flavored shapes / API sketches (observability event/contract shapes)

The principle (from the Harbor Console model): the Dockyard server runtime is **headless** and emits a canonical, versioned event stream. The inspector and the future console are *pure clients* of that stream — they never read runtime internals.

### 3.1 Canonical observability event

```go
// Package obs — Dockyard's observability protocol. Stable, versioned, the
// ONLY thing inspector + console consume. No internal runtime types leak.
type Event struct {
    SchemaVersion string    `json:"schema_version"` // e.g. "dockyard.obs/v1"
    ID            string    `json:"id"`
    Timestamp     time.Time `json:"timestamp"`
    ServerID      string    `json:"server_id"`     // stable identity of this server
    SessionID     string    `json:"session_id"`    // MCP session
    TraceID       string    `json:"trace_id"`      // correlates a call chain
    SpanID        string    `json:"span_id"`
    ParentSpanID  string    `json:"parent_span_id,omitempty"`

    Kind    EventKind       `json:"kind"`
    Phase   Phase           `json:"phase"`          // start | end | progress | emit
    Payload json.RawMessage `json:"payload"`        // typed per Kind (see §3.2)

    DurationMS *int64     `json:"duration_ms,omitempty"` // set on Phase=end
    Error      *ErrorInfo `json:"error,omitempty"`
}

type EventKind string

const (
    KindToolCall       EventKind = "tool.call"        // tools/call lifecycle
    KindResourceRead   EventKind = "resource.read"    // resources/read
    KindPromptGet      EventKind = "prompt.get"       // prompts/get
    KindAppLoad        EventKind = "app.load"         // ui:// resource served to host
    KindAppBridge      EventKind = "app.bridge"       // ui/initialize handshake, bridge up/down
    KindUserAction     EventKind = "app.user_action"  // action dispatched from the App UI
    KindHostCompat     EventKind = "host.compat"      // detected host capability / incompat
    KindLog            EventKind = "log"              // bridges MCP notifications/message
    KindServerLifecycle EventKind = "server.lifecycle" // start, stop, capability negotiation
)

type Phase string

const (
    PhaseStart    Phase = "start"
    PhaseEnd      Phase = "end"
    PhaseProgress Phase = "progress" // long-running tasks (ext-tasks)
    PhaseEmit     Phase = "emit"     // point-in-time event
)

type ErrorInfo struct {
    Type      string `json:"type"`       // maps to OTel error.type
    Message   string `json:"message"`
    Retryable bool   `json:"retryable"`
    Silent    bool   `json:"silent"`     // protocol-masked failure (the Sentry insight)
}
```

### 3.2 Per-kind payloads (illustrative)

```go
type ToolCallPayload struct {
    Tool       string          `json:"tool"`
    Transport  string          `json:"transport"`         // stdio | http | sse
    Client     string          `json:"client,omitempty"`  // client name from initialize
    InputRedacted  json.RawMessage `json:"input,omitempty"`  // opt-in, redaction-aware
    OutputRedacted json.RawMessage `json:"output,omitempty"` // opt-in
    OutputBytes    int             `json:"output_bytes"`     // size guardrail signal
    ContractOK     bool            `json:"contract_ok"`      // input/output validated vs schema
}

type AppLoadPayload struct {
    AppID       string `json:"app_id"`
    ResourceURI string `json:"resource_uri"` // ui://...
    MIME        string `json:"mime"`         // text/html;profile=mcp-app
    BridgeReady bool   `json:"bridge_ready"` // did ui/initialize complete?
}

type HostCompatPayload struct {
    Host          string   `json:"host"`            // claude | vscode | goose ...
    Capability    string   `json:"capability"`      // e.g. "apps", "tasks"
    Supported     bool     `json:"supported"`
    Degradation   string   `json:"degradation,omitempty"` // human-readable verdict
}
```

### 3.3 Emitter (in-process, headless) + transport

```go
// The runtime depends only on this interface. Inspector/console are clients.
type Emitter interface {
    Emit(ctx context.Context, e Event)
}

// V1 ships at least:
//   - RingBufferEmitter : in-memory, last N events, what the inspector reads
//   - SSESink           : streams obs/v1 events out-of-band (NOT the stdio pipe)
//   - OTelEmitter       : maps Event -> OTel spans using mcp.* + gen_ai.* semconv
//                         (export is OPTIONAL; never a prerequisite to observe)
```

The inspector consumes the obs/v1 stream over a local out-of-band channel (HTTP/SSE on a dev port) so stdio servers stay debuggable without corrupting the protocol pipe. The post-V1 multi-server console consumes the *same* obs/v1 contract from many servers — no new protocol, only fan-in.

### 3.4 OTel mapping (interoperability, not dependency)

`Event` is designed to lower cleanly onto OTel MCP semconv: `tool.call` → `span.mcp.server` named `tools/call {tool}` with `mcp.method.name`, `gen_ai.tool.name`, `gen_ai.operation.name=execute_tool`, `mcp.session.id`, `network.transport`, `error.type`; `resource.read` → `mcp.resource.uri`. This lets a Dockyard server slot into Datadog/Sentry/Grafana *if the team wants it* — without making any of them required to use the inspector.

## 4. Sharp edges & risks

1. **OTel MCP semconv is still "Development."** Attribute names may shift. Mitigation: treat OTel as an *export adapter* behind `obs/v1`, never as the internal model. `obs/v1` is Dockyard's stable contract; the adapter absorbs churn.
2. **stdio out-of-band channel.** The inspector needs an out-of-band sink, but opening a dev port has security implications (cf. CVE-2025-49596 RCE in the official Inspector's proxy). The obs sink must be localhost-only, read-only, dev-mode-gated, and never proxy arbitrary execution.
3. **Sensitive payloads.** Tool input/output and user actions can carry secrets/PII. `obs/v1` makes argument/result capture *opt-in and redaction-aware* (mirroring OTel's opt-in `gen_ai.tool.call.arguments`). Default: capture shape and size, not content.
4. **App half-visibility.** Dockyard only sees the server side of the iframe bridge. It can report "served `ui://` resource, bridge handshake not received within T" but cannot see host-side mount failures. Frame this honestly: the inspector emulates the host bridge (like mcp-use) for local debugging, and `host.compat` events flag suspected incompat — but production host bugs (ext-apps #482) remain partly opaque.
5. **Console scope creep.** A multi-server console is a natural ask. Decision per braindump: V1 = single-server inspector + `obs/v1` protocol only; multi-server fleet console is post-V1, and must remain a pure `obs/v1` client so it is purely additive.
6. **MCP `logging` capability overlap.** `obs/v1` `KindLog` should bridge `notifications/message` rather than compete with it — a Dockyard server still speaks standard MCP logging to any client; `obs/v1` is the richer superset for Dockyard's own tools.

## 5. What Dockyard must adopt / build / avoid

**Adopt:**
- OTel MCP semantic conventions (`mcp.*`, `gen_ai.*`, `span.mcp.server`) as the *export vocabulary* and as naming guidance for `obs/v1`.
- The MCP `logging` capability — bridge it, don't bypass it.
- mcp-use's inspector DX bar: widget emulator, host-bridge emulation, device/locale/CSP testing, RPC logger with filtering.
- Sentry's framing of "silent, protocol-masked failures" as a first-class signal (`ErrorInfo.Silent`).

**Build (V1):**
- `obs/v1` — a canonical, versioned, headless observability event protocol; the runtime emits it, the inspector consumes it. This is the "observability as a protocol" deliverable.
- A local single-server **inspector** that surfaces: live `obs/v1` event stream + RPC log; per-tool latency/error/volume analytics; MCP App rendering with host-bridge emulation, device/locale/CSP testing; fixture selector (happy/empty/error/permission/slow/large) wired to generated contracts; contract-drift and schema-validation verdicts; host-compatibility verdicts; structured-log view bridging `notifications/message`.
- An optional `OTelEmitter` export adapter (off by default).

**Avoid:**
- The MCP Mesh anti-pattern: observability that only works once you deploy and operate an external stack (Grafana/Tempo/Redis). Dockyard observability must be intrinsic and zero-dependency — useful for a one-server, ten-minute developer with nothing else installed.
- Any cloud-only control plane, hosted-registry lock-in, or "real observability requires our managed service" gating. The full inspector + `obs/v1` ship in the OSS binary.
- Letting the inspector become a production-facing client or an RCE surface (CVE-2025-49596): dev-mode-gated, localhost-only, read-only.

## 6. Open questions (Q-N — feed the RFC open-questions section)

- **Q-1:** Out-of-band transport for the local obs stream — local HTTP+SSE on a dev port, a Unix domain socket, or a tailed structured-log file? Security and cross-platform (Windows) behavior differ.
- **Q-2:** Does `obs/v1` get versioned and documented as a *public, third-party-consumable* contract in V1, or kept internal-but-stable until the post-V1 console ships?
- **Q-3:** Default capture policy for tool input/output — shape+size only, or opt-in full content with a redaction pipeline? What is the redaction API surface?
- **Q-4:** Should `obs/v1` define its own trace/span IDs, or adopt W3C Trace Context so Dockyard spans nest natively under a Harbor agent's `execute_tool` span?
- **Q-5:** Is the OTel export adapter V1 scope or post-V1? It is cheap given `obs/v1`, but adds a dependency surface.
- **Q-6:** How does the inspector emulate host-specific quirks (Claude vs VS Code vs Goose) for `host.compat` verdicts — a static capability matrix, or recorded host fixtures? How is the matrix kept current as hosts evolve?
- **Q-7:** Does the multi-server console (post-V1) live in the Dockyard repo as a satellite, or is fleet observability folded into Portico's gateway observability? Both consume `obs/v1`, but ownership affects packaging.
- **Q-8:** ext-tasks progress events — should long-running task `Phase=progress` events be part of `obs/v1` V1, given Tasks is in V1 scope per Dump 3?

## 7. Sources

- mcp-mesh.ai — homepage (reachable). https://mcp-mesh.ai/
- mcp-mesh.ai/pricing — **HTTP 404, no public pricing page exists**. https://mcp-mesh.ai/pricing
- MCP Mesh observability docs. https://mcp-mesh.ai/07-observability/
- OpenTelemetry — Semantic conventions for Model Context Protocol (MCP). https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/
- OpenTelemetry — GenAI agent & framework spans. https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/
- Sentry — Introducing MCP server monitoring. https://blog.sentry.io/introducing-mcp-server-monitoring/
- OneUptime — Instrumenting MCP servers with OpenTelemetry (2026-03-26). https://oneuptime.com/blog/post/2026-03-26-how-to-instrument-mcp-servers-with-opentelemetry/view
- Official MCP Inspector. https://github.com/modelcontextprotocol/inspector — and https://modelcontextprotocol.io/docs/tools/inspector
- mcp-use inspector. https://github.com/mcp-use/inspector — widget debugging: https://mcp-use.com/docs/inspector/debugging-chatgpt-apps
- MCPJam inspector. https://github.com/MCPJam/inspector
- mark3labs/mcp-go. https://github.com/mark3labs/mcp-go
- Official Go SDK. https://github.com/modelcontextprotocol/go-sdk
- MCP logging specification. https://modelcontextprotocol.io/specification/2025-03-26/server/utilities/logging
- MCP debugging guide. https://modelcontextprotocol.io/docs/tools/debugging
- MCP Apps extension overview & spec. https://modelcontextprotocol.io/extensions/apps/overview — spec: https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx
- ext-apps issue #482 (host-compat: widget not mounting after resources/read). https://github.com/modelcontextprotocol/ext-apps/issues/482
