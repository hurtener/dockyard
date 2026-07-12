# Phase 33 — Apps, Tasks, and multi-round-trip requests

## Summary

Migrate Dockyard's extension surfaces to the `2026-07-28` stateless lifecycle.
Apps advertise through discovery; Tasks gains its revised extension lifecycle;
Dockyard's bespoke input-supply method is replaced by standards-based retryable
multi-round-trip requests while legacy peers retain their versioned path.

**Documentation/hygiene status (2026-07-11):** User-facing Apps, inspector, and
approval-flow documentation now distinguishes core MRTR retries from task
mid-flight `tasks/update`; glossary and decision records are synchronized, and
the phase smoke script has concrete checks. Acceptance criteria remain unchecked
until their implementation and verification gates have run.

## RFC anchor

- RFC §7
- RFC §8
- RFC §16
- RFC §19.1

## Briefs informing this phase

- brief 07
- brief 01
- brief 02
- brief 03

## Brief findings incorporated

- **brief 07 §1.3:** Tasks and interaction are reshaped for stateless requests;
  `tasks/list` does not survive in the modern lifecycle.
- **brief 02 §4.5:** task access must remain bound to an identifiable requestor.
- **brief 01 §4:** capabilities are protocol-driven, never host-matrix-driven.

## Findings I'm departing from (if any)

None. D-071/D-072 are superseded only for the `2026-07-28` codec; their legacy
behavior remains readable for compatible peers.

## Goals

- Emit Apps and extension capability information in `server/discover`.
- Replace raw modern Tasks interception with the SDK's custom-method seam.
- Support modern task operations and core MRTR without retaining protocol
  session state, while keeping their continuation mechanisms distinct.

## Non-goals

- OAuth token validation (Phase 36).
- Removing the legacy Tasks path before its protocol version is retired upstream.
- Adding host-specific interaction branches.

## Acceptance criteria

- [x] A modern `server/discover` response advertises Apps and Tasks capabilities;
      legacy initialize output remains correct for legacy peers.
- [x] Modern Tasks supports `tasks/get`, `tasks/update`, and `tasks/cancel`; it
      does not advertise or accept `tasks/list`.
- [x] A task-augmented tool returns a standards-shaped task handle and preserves
      identity binding across independently routed requests.
- [x] A core-MRTR input-required operation returns retryable request state; a
      retry of the original method with input responses completes without
      `dockyard/tasks/supplyInput`.
- [x] A task that requires mid-flight input exposes `inputRequests` through
      `tasks/get` and accepts matching `inputResponses` through `tasks/update`;
      it does not use core MRTR request state.
- [x] Legacy `tasks/*` and input-supply behavior remain isolated behind the
      `2025-11-25` codec and do not contaminate modern frames.
- [x] The approval-flows template and inspector exercise the modern lifecycle end
      to end under `-race`.

## Files added or changed

- `internal/protocolcodec/tasks.go`, `codec.go`, `version.go`, goldens/fuzz tests
- `runtime/tasks/*`, including transport, capability, engine, dispatch, security
- `runtime/server/server.go`, `http.go`, `stdio.go`
- `runtime/apps/*`, capability tests
- `web/bridge/*` where MRTR changes the View/host contract
- `templates/approval-flows/*`, `test/integration/phase33_*`
- `internal/inspector/elicitation.go` and tests
- `docs/site/`, `skills/attach-a-ui-resource/SKILL.md`, `skills/test-with-the-inspector/SKILL.md`
- `docs/plans/phase-33-apps-tasks-mrtr.md`
- `scripts/smoke/phase-33.sh`

## Public API surface

```go
type InputRequest struct { /* typed MRTR request */ }
type RequestState string

func (h *TaskHandle) RequestInput(ctx context.Context, req InputRequest) error
```

`RequestState` belongs to core MRTR and is not stored on a Task. The exact public
task API is designed from the vendored final extension schema; handlers never
receive raw extension wire envelopes.

## Design gate

- Derive modern Tasks from the Phase 31 Tasks pin and core MRTR from the core
  draft schema and golden fixtures, including the explicit legacy codec
  boundary. The approved design is recorded in
  `docs/plans/phase-33-protocol-design.md`.
- The design owner approves the migration note, handler API, and App-bridge
  interaction before implementation; no raw-frame workaround is retained by
  default merely because it served the legacy protocol.

## Test plan

- **Unit:** per-version codec decode/encode, state transitions, request-state
  integrity, task authorization, modern/legacy capability construction.
- **Integration:** real SDK modern and legacy clients drive Apps and Tasks over
  HTTP; approval-flow MRTR covers success, invalid retry, and cancellation.
- **Concurrency / golden:** task-store concurrent reuse under `-race`; schema-pinned
  wire goldens for both codecs; fuzz all raw-frame/custom-method decoders.

## Smoke script additions

- Assert modern Tasks/MRTR tests, legacy codec tests, and approval-flow integration
  coverage exist.

## Coverage target

- `runtime/tasks`: 85% conformance band.
- `runtime/apps`: 85% conformance band.
- `internal/protocolcodec`: 85% conformance band.

## Dependencies

- 31 — SDK foundation.
- 32 — dual-lifecycle HTTP.
- 09, 11, 13, 14 — existing Apps, bridge, and Tasks surfaces.

## Risks / open questions

- The final Tasks extension schema and final core MRTR schema are authoritative;
  no conformance claim is fixed until both are vendored and golden-pinned.
- The current approval-flow App protocol may need a new bridge request shape;
  update UI docs/skills in the same PR.

## Glossary additions

- Multi Round-Trip Request.
- Request state.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `npx markdownlint-cli2 "**/*.md" "!**/node_modules"` passes
- [x] `make docs` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
