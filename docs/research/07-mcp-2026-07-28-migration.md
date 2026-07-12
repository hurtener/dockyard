# Brief 07 — MCP 2026-07-28 migration

**Date:** 2026-07-10
**Sources:** MCP `2026-07-28` release-candidate announcement and draft
specification; Go SDK `v1.7.0-pre.2` release guidance. Retrieved 2026-07-10.
**Status:** Informing V2 phases 31–36.

## 1. Findings

### 1.1 The core lifecycle is stateless

The revision removes `initialize`, `notifications/initialized`, and
`Mcp-Session-Id`. A `2026-07-28` request carries the protocol version, client
information, and client capabilities in request metadata; `server/discover`
returns server capabilities. The protocol is stateless, but applications retain
state through explicit handles and durable stores.

**Implication:** HTTP authorization and requestor identity must be validated and
derived per request. `obs/v1` must not fabricate a session ID for a stateless
call.

### 1.2 The Go SDK requires an explicit stateless transport choice

The Go prerelease is `github.com/modelcontextprotocol/go-sdk v1.7.0-pre.2`.
Serving `2026-07-28` requires `StreamableHTTPOptions.Stateless = true`; a
stateful handler continues to speak the legacy lifecycle. The SDK update alone
does not migrate a Dockyard server.

**Implication:** Dockyard owns a version-aware HTTP dispatcher and must prove its
one endpoint supports both revisions without a body-inspection downgrade path.

### 1.3 Tasks and server-to-client interaction change shape

Tasks moves to an extension lifecycle suited to stateless requests. The old
`tasks/list` surface disappears; task state is driven through `tasks/get`,
`tasks/update`, and `tasks/cancel`. Core Multi Round-Trip Requests replace
long-lived-stream interaction by returning input requests plus retryable request
state. Task mid-flight input is related but distinct: `tasks/get` returns
outstanding input requests and `tasks/update` accepts their responses without
retrying the original method or carrying core request state.

**Implication:** Dockyard's raw `tasks/*` mount, initialize-response rewrite, and
`dockyard/tasks/supplyInput` extension require a versioned migration behind
`internal/protocolcodec`. Existing Tasks must remain available only to legacy
peers while supported. Phase 31 must vendor the authoritative revised Tasks
extension schema before Phase 33 fixes its public API, and must record whether the
Apps extension snapshot changed for this core revision.

### 1.4 Server response semantics broaden

The revision makes routing headers (`Mcp-Method`, `Mcp-Name`) normative, adds
list/resource caching data, changes the missing-resource error to JSON-RPC
`-32602`, and moves tool schemas to full JSON Schema 2020-12. The latter conflicts
with Dockyard's documented V1 recursive-contract limitation.

**Implication:** codegen, tool output restrictions, cache semantics, and error
tests are part of the migration; the protocol target cannot be claimed by an HTTP
transport-only change.

### 1.5 The final release remains a release gate

The RC was locked on 2026-05-21; the final is scheduled for 2026-07-28. The SDK
guidance requires exact prerelease pins during validation and acknowledges public
API changes before stable release.

**Implication:** vendoring, pinning, and a final normative diff are explicit
acceptance criteria, not release chores.

## 2. Sources

- MCP release candidate: <https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/>
- Go SDK beta guidance: <https://blog.modelcontextprotocol.io/posts/sdk-betas-2026-07-28/>
- Draft specification: <https://modelcontextprotocol.io/specification/draft>
- Draft changelog: <https://modelcontextprotocol.io/specification/draft/changelog>

## 3. Avoid

- Treating `go get` as protocol migration.
- Making a host capability matrix instead of selecting by protocol version.
- Deriving authorization or application state from a removed session header.
- Editing the legacy Tasks codec in place instead of adding a versioned codec.
- Claiming final conformance from an RC pin without a final-spec diff.
