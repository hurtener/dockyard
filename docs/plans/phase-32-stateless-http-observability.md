# Phase 32 — Dual-lifecycle HTTP and observability

## Summary

Serve MCP `2026-07-28` stateless HTTP and `2025-11-25` legacy HTTP from one
endpoint. Move base-protocol client metadata, capability context, logging level,
and observability handling to the request boundary without changing `obs/v1`.

## RFC anchor

- RFC §5.2
- RFC §11.2
- RFC §16
- RFC §19.1

## Briefs informing this phase

- brief 07
- brief 03
- brief 05

## Brief findings incorporated

- **brief 07 §1.1:** request identity and authorization are per-request in the
  stateless lifecycle; session IDs cannot be fabricated.
- **brief 07 §1.2:** version selection must not inspect the request body.
- **brief 03 §2.3:** HTTP security options remain explicit Dockyard middleware.

## Findings I'm departing from (if any)

None.

## Goals

- Make the stateless lifecycle the modern HTTP path while preserving legacy peers
  on the same endpoint.
- Keep existing DNS-rebinding, Origin, and Content-Type defenses outermost.
- Preserve `obs/v1` compatibility while removing session assumptions from modern
  HTTP requests.

## Non-goals

- Apps/Tasks/MRTR migration (Phase 33).
- OAuth bearer validation (Phase 36).
- Changing the `obs/v1` event schema version.

## Acceptance criteria

- [ ] One root-mounted handler accepts an SDK `2026-07-28` request using
      `server/discover` without `Mcp-Session-Id`.
- [ ] The same endpoint completes a real `2025-11-25` initialize flow while that
      protocol remains supported.
- [ ] A protocol-version header selects a handler before JSON-RPC decoding; an
      unsupported/missing modern version fails clearly and never downgrades.
- [ ] Modern requests validate routing headers against the request body and retain
      the explicit HTTP security posture.
- [ ] Handler context receives request-scoped client metadata and capabilities;
      stateless `obs/v1` events carry no fabricated session ID.
- [ ] Legacy logging behavior remains compatible; modern request-scoped log-level
      metadata is bridged to `obs/v1`.

## Files added or changed

- `runtime/server/http.go`, `server.go`, `tool.go`, `resource.go`, `prompt.go`
- `runtime/server/logbridge.go`, HTTP/obs tests
- `runtime/obs/*` tests only where optional session semantics are asserted
- `internal/scaffold/templates.go`, examples' HTTP entrypoints
- `docs/site/`, `skills/run-the-dev-loop/SKILL.md`, `skills/package/SKILL.md`
- `docs/plans/phase-32-stateless-http-observability.md`
- `scripts/smoke/phase-32.sh`

## Public API surface

```go
type ProtocolMode uint8 // Legacy, Stateless20260728, Dual

type HTTPOptions struct {
	ProtocolMode ProtocolMode
	// Stateless remains deprecated compatibility input.
	Stateless bool
}
```

The exact names are provisional until Phase 31 confirms the SDK multiplexing
surface; the API must express explicit version selection, not a host matrix.

## Test plan

- **Unit:** version dispatch, routing-header validation, request context, log-level
  extraction, optional session ID behavior.
- **Integration:** real SDK clients drive both protocol versions against one HTTP
  endpoint; the stateless path is exercised through a round-robin handler pair.
- **Concurrency / golden:** concurrent stateless calls under `-race`; header and
  response fixtures per protocol version.

## Smoke script additions

- Assert HTTP options expose explicit dual-lifecycle support and stateless tests
  exist; skip only before implementation.

## Coverage target

- `runtime/server`: 85% conformance band.
- `runtime/obs`: 85% conformance band.

## Dependencies

- 31 — SDK foundation.
- 07, 15, 16 — HTTP server and observability seams.

## Risks / open questions

- If one SDK handler cannot multiplex revisions, use a header-only dispatcher over
  two SDK handlers; do not expose separate public deployment endpoints without a
  superseding decision.
- Request correlation must use existing trace fields unless an `obs/v2` change is
  deliberately designed and documented.

## Glossary additions

- N/A — Phase 31 establishes the lifecycle term.

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
