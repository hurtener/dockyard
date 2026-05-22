# Phase 07 — MCP server core — transports + security

## Summary

Phase 07 brings `runtime/server` to the full MCP server core (RFC §5): typed
**resource** registration alongside the existing tool registration, the
**streamable-HTTP** transport beside stdio with its security options set
**explicitly**, the `getServer` per-request server seam, and `InMemoryTransport`
wired as the contract-test backbone. It also resolves D-021 by retiring the
temporary exported `Server.MCP()` SDK seam.

## RFC anchor

- RFC §5 — MCP server core
- RFC §5.1 — the SDK is the foundation
- RFC §5.2 — transports + explicit security
- RFC §5.3 — extension hooks
- RFC §5.4 — the `protocolcodec` isolation seam (P3)

## Briefs informing this phase

- brief 03 — Official Go MCP SDK audit
- brief 01 — MCP Apps extension

## Brief findings incorporated

- **Brief 03 §2.3** — "Security knobs: DNS-rebinding protection for localhost
  (default on since v1.4.0), Origin/Content-Type verification (v1.5.0),
  cross-origin protection (default **off** again as of v1.6.0 — Dockyard should
  re-enable explicitly for HTTP deployments)." Phase 07 sets all three
  explicitly in `HTTPSecurity` and never trusts an SDK default.
- **Brief 03 §2.3** — "HTTP entry points: `NewStreamableHTTPHandler(getServer
  func(*http.Request) *Server, opts)` … The `getServer` callback
  (server-per-request) is a natural seam for Dockyard's multi-tenant /
  per-session wiring." Phase 07 exposes `getServer` as the `ServerForRequest`
  option on `HTTPHandler`.
- **Brief 03 §2.3** — "`InMemoryTransport` (`NewInMemoryTransports()` — useful
  for the Dockyard inspector and contract tests)." Phase 07 wires it as
  `ServeInMemory` so later phases and tests get a transport-agnostic backbone.
- **Brief 01 §ui-resources** — MCP Apps serve their HTML bundle as a resource
  under the `ui://` scheme; the SDK ships first-class resources. Phase 07 lands
  the typed `AddResource` surface that the Wave 4 Apps work composes, so the
  Apps layer never reaches past the runtime to the raw SDK.

## Findings I'm departing from (if any)

None. Phase 07 implements brief 03's transport and security guidance directly.

## Goals

- Typed resource registration (`AddResource`) on the Dockyard server, with a
  `ResourceContent` return type that does not expose raw SDK structs.
- A streamable-HTTP transport (`HTTPHandler`) alongside stdio.
- HTTP security — DNS-rebinding protection, Origin/Content-Type verification,
  cross-origin protection — represented by an explicit, asserted `HTTPSecurity`
  option, never inherited from an SDK default.
- The `getServer` per-request server seam exposed as `ServerForRequest`.
- `InMemoryTransport` wired as `ServeInMemory` for tests and later phases.
- Resolve D-021: unexport the temporary `Server.MCP()` SDK seam.

## Non-goals

- The `Result`-semantics handler runtime — the `content`/`structuredContent`
  split and edge validation — is **Phase 08**.
- The empty-`TextContent` fix in `AddToolWithSchemas` flagged by the Wave 2
  audit belongs to **Phase 08**, not here.
- The Apps `ui://` layer (MIME profile, `_meta` linking, bridge) is **Phase 09**.
- SSE (legacy) transport — Dockyard V1 ships stdio + streamable-HTTP only
  (RFC §5.2).
- Stream-resumption `EventStore` and idle-session timeouts — not required by
  the acceptance criteria; left as later hardening.

## Acceptance criteria

- [ ] A server serves over **both** stdio and streamable-HTTP.
- [ ] Resources register and read back over a transport.
- [ ] HTTP security options (DNS-rebinding, Origin/Content-Type, cross-origin)
      are asserted explicitly set — a test proves it.
- [ ] The `getServer` per-request seam is exercised by a test.
- [ ] The `InMemoryTransport` path is tested.
- [ ] A concurrent-reuse test proves the server is safe under concurrent use.
- [ ] D-021 resolved: `Server.MCP()` is unexported (no external consumer).
- [ ] Resource templates register and read back over a transport (D-054).
- [ ] A panicking tool or resource handler, called over a real transport,
      returns an error result and the server process survives (D-053).

> **Depth-remediation addendum (2026-05-21).** A 4-way audit found two gaps in
> the surface this phase shipped. (1) Panic safety was enforced only on the
> registration path (`addToolSafe`/`addResourceSafe`); the handler-invocation
> path was unguarded, so a panicking tool/resource handler crashed the server
> process — fixed by `guardHandler` (`recover.go`), which `recover()`s every
> tool and resource handler invocation into a typed error (D-053). (2)
> `AddResourceTemplate` was not exposed despite the SDK offering it and Phase 10
> needing it for `ui://` auto-discovery — added as a typed, panic-recovered
> surface consistent with `AddResource` (D-054).

## Files added or changed

```text
runtime/server/
  doc.go            (changed — document transports + resources)
  server.go         (changed — MCP() -> mcp(); doc updates)
  resource.go       (new — typed resource registration)
  resource_test.go  (new)
  http.go           (new — streamable-HTTP handler + explicit security)
  http_test.go      (new)
  server_test.go    (changed — drop MCP() assertion)
docs/plans/phase-07-server-core.md   (new — this file)
docs/decisions.md                    (changed — D-040, D-041, D-042)
docs/glossary.md                     (changed — new vocabulary)
scripts/smoke/phase-07.sh            (new)
```

## Public API surface

```go
// Resources (resource.go)
type ResourceDef struct { URI, Name, Title, Description, MIMEType string }
type ResourceContent struct { MIMEType string; Text string; Blob []byte }
type ResourceFunc func(ctx context.Context, uri string) (ResourceContent, error)
func (s *Server) AddResource(def ResourceDef, fn ResourceFunc) error
func (s *Server) Resources() []string

// Resource templates — RFC 6570 URI-template families (resource.go; D-054)
type ResourceTemplateDef struct { URITemplate, Name, Title, Description, MIMEType string }
func (s *Server) AddResourceTemplate(def ResourceTemplateDef, fn ResourceFunc) error
func (s *Server) ResourceTemplates() []string

// Panic safety — every handler-invocation path is recover-wrapped (recover.go; D-053)
var ErrHandlerPanic error

// Streamable-HTTP transport + explicit security (http.go)
type HTTPSecurity struct {
    DNSRebindingProtection bool
    CrossOriginProtection  bool
    TrustedOrigins         []string
}
func DefaultHTTPSecurity() HTTPSecurity   // all protections ON
type HTTPOptions struct {
    Security        HTTPSecurity
    ServerForRequest func(*http.Request) *Server  // getServer per-request seam
    Stateless       bool
}
func (s *Server) HTTPHandler(opts *HTTPOptions) (http.Handler, error)

// In-memory transport (server.go)
func (s *Server) ServeInMemory(ctx context.Context) mcpsdk.Transport
```

## Test plan

- **Unit:** resource registration validation (nil server, empty URI, empty
  name, nil handler, duplicate URI); `HTTPSecurity` default-on assertion;
  `HTTPHandler` rejects a nil `ServerForRequest`-less call gracefully; `MCP()`
  unexport compiles.
- **Integration:** a server serves over the real streamable-HTTP handler via
  `httptest.Server` and an SDK `StreamableClientTransport`, lists + reads a
  resource, and calls a tool — proving the HTTP path end-to-end with a real
  client (AGENTS.md §17). Resource read-back over `InMemoryTransport`.
- **Concurrency / golden:** `TestConcurrentResourceReads` covers concurrent
  resource reads and `TestPhase07_ConcurrentHTTPSessions` covers concurrent
  HTTP sessions, both under `-race`. No golden output in this phase.

## Smoke script additions

- `runtime/server/http.go` and `resource.go` exist.
- The server package builds CGo-free.
- The server package tests pass (covers transports, resources, security).
- `Server.MCP()` is no longer an exported method (D-021 resolved).
- `HTTPSecurity` defaults assert all protections on.

## Coverage target

- `runtime/server` — **85%** (the phase's stated target; the server is a
  conformance-tested reusable subsystem).

## Dependencies

- Phase 01 — `runtime/server` (the package this phase extends).
- Phase 02 — `internal/protocolcodec` (the wire-format seam; no code coupling
  this phase, but the dependency is declared in the master plan).

## Risks / open questions

- The SDK's `StreamableHTTPOptions.CrossOriginProtection` field is *deprecated*
  in v1.6.0 in favour of wrapping the handler with
  `http.CrossOriginProtection` middleware. Phase 07 follows the SDK's
  recommended middleware approach so the choice is explicit and survives the
  field's removal — recorded as D-041.
- DNS-rebinding protection is on-by-default in the SDK and *disabled* via
  `DisableLocalhostProtection`. Dockyard's `HTTPSecurity.DNSRebindingProtection`
  is a positive-sense flag mapped to that negative SDK knob, set explicitly so a
  future SDK default flip cannot silently change behaviour — recorded as D-040.

## Glossary additions

- **Streamable-HTTP transport** — added to `docs/glossary.md`.
- **`getServer` seam** — added to `docs/glossary.md`.
- **HTTP security options** — added to `docs/glossary.md`.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

## Remediation notes

- **R3 / depth audit (D-112).** This plan's Brief findings and acceptance
  criteria call for DNS-rebinding, Origin/Content-Type, and cross-origin
  protections all set explicitly in `HTTPSecurity`. The depth audit found
  Content-Type verification was the one not actually delivered — `HTTPSecurity`
  set DNS-rebinding and cross-origin/Origin explicitly but left Content-Type to
  whatever the linked go-sdk defaults to, contradicting "never inherited from an
  SDK default" (CLAUDE.md §7). R3 closes the gap: `HTTPSecurity` gains an
  explicit `ContentTypeVerification` field (on in `DefaultHTTPSecurity`), and
  `HTTPHandler` wraps a Dockyard `contentTypeMiddleware` that rejects a
  non-`application/json` POST body with 415. A behavioural test asserts it via
  the middleware's distinct rejection body. See D-112.
