# Plan — Thread inbound request `_meta` to typed tool handlers

> Short, phase-style plan for an additive, non-phase change. Not a numbered
> phase: no new subsystem, no manifest field, no CLI verb. It extends one
> existing runtime seam (`runtime/server`) by analogy to `RawArguments`.

- **Type:** additive · no SDK change · no breaking change
- **Release:** minor — `v1.8.0`
- **Owning subsystem:** `runtime/server` (the MCP server core, RFC §5)
- **RFC:** §5 (server core), §5.4 / P3 (protocol isolation), §6.3 (handler I/O)
- **Decision:** new `D-189` (the inbound-`_meta` read-only handler seam)

---

## Problem

A typed tool handler — `func(ctx, in In) (Result[Out], error)` — receives only
the decoded `params.arguments` (as `In`) and the transport session (on `ctx`).
The sibling `params._meta` (`CallToolParamsRaw.Meta`) is on the request object
the runtime already holds (`runtime/server/tool.go`), but it is never surfaced,
so a handler has no path to host-injected, per-call context (e.g. `user`,
`session`, `agent_id`) that a host attaches outside the model-filled
`arguments`.

Dockyard is a **pure read-only consumer** of this map: it surfaces whatever the
host sent, verbatim and opaque. It does **not** inject, pre-populate, derive, or
inspect any key — the keys are the host's contract with the app, not Dockyard's.
This is what keeps the change P3-clean: the runtime bakes in zero knowledge of
any `_meta` key shape.

## Approach

Mirror the existing `WithRawArguments` / `RawArguments` seam (`tool.go`) exactly:

1. **New context key + accessor pair** beside `RawArguments`:
   - `RequestMeta(ctx) map[string]any` — returns the inbound `_meta`, or nil.
   - `WithRequestMeta(ctx, map[string]any) context.Context` — stashes it; a
     nil/empty map is a no-op (returns `ctx` unchanged).
   - The accessor type is **`map[string]any`, not `mcpsdk.Meta`** — consistent
     with the existing public `_meta` surfaces (`ToolDef.Meta`, `ToolOutput.Meta`
     are both `map[string]any`) and with P3 / §13 ("handler-facing APIs never
     expose raw protocol structs"). `mcpsdk.Meta` is `map[string]any` underneath,
     so threading `req.Params.Meta` in is a zero-cost assignment.
   - `WithRequestMeta` stores a **per-call shallow copy** so a handler mutating
     the returned map cannot reach the in-flight protocol state (the inbound map
     carries protocol-reserved keys such as `progressToken`). This is the one
     deliberate hardening beyond `RawArguments`, which hands its slice through
     directly; for `_meta` the map is mutable and shared with the SDK, so the
     copy is warranted. Reuses the existing `cloneMeta` shape.

2. **Thread it in both wrappers** (`AddTool` and `AddToolWithSchemas`):

   ```go
   if req != nil && req.Params != nil {
       ctx = WithRequestMeta(ctx, req.Params.Meta) // beside WithRawArguments
   }
   ```

   `AddTool` currently threads no request params at all (only the session); it
   gains the guarded block. `AddToolWithSchemas` already has the guarded block
   around `WithRawArguments` — the meta line joins it.

## MCP compliance

- Reading `params._meta` is spec-compliant; we emit nothing, so no `protocolcodec`
  outbound-encoding rule (D-046/D-048) is touched.
- The inbound map MAY carry protocol-reserved keys (`progressToken`, reserved
  prefixes). We hand it back read-only and copied — a handler cannot corrupt the
  reserved machinery. The accessor godoc states the read-only/don't-retain
  contract, mirroring `RawArguments`.

## Files added or changed

- `runtime/server/tool.go` — new `requestMetaKey`, `RequestMeta`,
  `WithRequestMeta`; thread into both wrappers.
- `runtime/server/requestmeta_test.go` — round-trip, no-op branches,
  defensive-copy isolation, and an end-to-end assertion that a `tools/call`
  carrying `_meta` reaches the handler via `RequestMeta` (real wrapper path).
- `scripts/smoke/v1.8-wave-A.sh` — smoke for the new public API surface
  (§4.2: a new public runtime API ⇒ a smoke check in the same PR).
- `docs/decisions.md` — `D-189`.
- `docs/glossary.md` — "request `_meta`" / `RequestMeta` entry.
- `CHANGELOG.md` — `[1.8.0]` section.

## Acceptance criteria

1. `RequestMeta(ctx)` returns the map stashed by `WithRequestMeta`, verbatim.
2. `WithRequestMeta(ctx, nil)` and `WithRequestMeta(ctx, map{})` are no-ops.
3. Mutating the map returned by `RequestMeta` does not mutate the caller's source
   map (defensive copy proven).
4. A `tools/call` request carrying `_meta` reaches the handler: the handler reads
   the injected keys via `RequestMeta`; a call with no `_meta` yields nil.
5. `go test -race ./runtime/server/...` clean; coverage band for
   `runtime/server` still met.
6. `scripts/smoke/v1.8-wave-A.sh` reports `OK ≥ count(criteria)`, `FAIL = 0`.
7. `make drift-audit`, `make check-mirror`, `make preflight` pass.

## Non-goals

- No typed wrapper over `_meta` keys (no `user`/`session`/`agent_id` struct) —
  that would bake host key shapes into Dockyard and break P3. Apps read raw keys.
- No outbound `_meta` change; no `protocolcodec` change.
- Resource/prompt request `_meta` is out of scope for this change (tool handlers
  only); a follow-up can extend the seam to those edges if a need appears.
