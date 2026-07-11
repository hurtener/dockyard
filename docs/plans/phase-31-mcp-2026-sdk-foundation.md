# Phase 31 — MCP 2026-07-28 SDK foundation

## Summary

Establish Dockyard's pinned, vendored, and tested foundation for the MCP
`2026-07-28` release candidate. This phase proves the SDK surface, vendors the
revised Tasks schema, records the Apps-spec audit, and establishes the dual-
lifecycle contract; it does not expose the stateless HTTP runtime yet.

## RFC anchor

- RFC §16
- RFC §19.1

## Briefs informing this phase

- brief 07
- brief 03
- brief 06

## Brief findings incorporated

- **brief 07 §1.2:** the Go SDK needs an explicit stateless transport choice;
  upgrading a module alone is not a protocol migration.
- **brief 07 §1.5:** RC pinning requires a final-spec re-pin and normative diff.
- **brief 03 §2.1 / §4:** the official SDK remains the CGo-free protocol core;
  Dockyard does not fork it.

## Findings I'm departing from (if any)

None.

## Goals

- Pin the RC SDK exactly and vendor the core draft specification that Dockyard
  will consume.
- Define and test the supported protocol-version compatibility contract before
  any runtime code changes.
- Make final-release reconciliation a binding phase deliverable.

## Non-goals

- Serving stateless HTTP requests (Phase 32).
- Apps, Tasks, MRTR, schema, inspector, or OAuth implementation.
- Claiming final `2026-07-28` conformance before the final spec is vendored.

## Acceptance criteria

- [x] `go.mod` pins `github.com/modelcontextprotocol/go-sdk` to the exact approved
      prerelease and the CGo-free build still passes.
- [x] The core draft specification and authorization draft are vendored with
      upstream SHA/date, indexed, and covered by a presence test.
- [x] The authoritative revised Tasks extension schema is vendored with upstream
      SHA/date and a golden-test entrypoint; the Apps extension is re-pinned to a
      revised snapshot or its unchanged revision is recorded with evidence.
- [x] A compatibility test proves the SDK's legacy and stateless handler modes
      are distinguishable without inspecting a JSON-RPC body.
- [x] A documented finalization checklist re-pins the stable SDK, vendors the
      final spec, runs a normative diff, and assigns every delta to this wave.
- [x] D-190, RFC §19.1, the glossary, and the master plan agree on dual-lifecycle
      support and RC status.

## Files added or changed

- `go.mod`, `go.sum`
- `docs/specifications/mcp-core-2026-07-28.mdx`
- `docs/specifications/mcp-authorization-2026-07-28.mdx`
- `docs/specifications/mcp-tasks-2026-07-28.*`
- `docs/specifications/mcp-apps-2026-07-28-audit.md`
- `docs/specifications/README.md`
- `docs/research/07-mcp-2026-07-28-migration.md`
- `internal/protocolcodec/version.go`, version tests and goldens
- `runtime/server/sdk_2026_test.go`
- `docs/plans/phase-31-mcp-2026-sdk-foundation.md`
- `scripts/smoke/phase-31.sh`

## Public API surface

- No new app-facing API. The phase records supported protocol versions internally
  for Phase 32's public HTTP options.

## Design gate

- Inspect the exact pinned SDK artifact and vendored core, Apps, and Tasks sources;
  write a compatibility note covering one-endpoint lifecycle multiplexing,
  stateless-method restrictions, and extension discovery.
- Obtain design-owner approval before changing the module pin or recording a
  public HTTP surface for Phase 32.

## Test plan

- **Unit:** SDK version/capability probes; vendored-spec presence and pin tests.
- **Integration:** real SDK legacy and stateless transports exercise the same
  minimal Dockyard server without body-based version selection.
- **Concurrency / golden:** protocol-version codec golden plus Tasks schema
  presence/pin test; no concurrent artifact changes.

## Smoke script additions

- Assert the plan, smoke script, both vendored core specs, and RC SDK pin exist.
- Assert the revised Tasks snapshot and Apps audit record exist.
- Skip implementation-only checks until Phase 32 lands.

## Coverage target

- `internal/protocolcodec`: 85% conformance band, preserved or raised.
- `runtime/server`: 85% conformance band, preserved or raised.

## Dependencies

- 02 — protocolcodec seam.
- 07 — server core.
- 27 — security/spec conformance pass.

## Risks / open questions

- The SDK prerelease API can change before final; Phase 31 ends RC-conformant with
  a finalization checklist, not a final-conformance claim.
- Whether the SDK can multiplex both lifecycles under one handler is proven in
  this phase before Phase 32 commits a public API.

## Glossary additions

- Stateless MCP lifecycle.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [ ] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
