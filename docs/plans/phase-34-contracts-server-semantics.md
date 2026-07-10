# Phase 34 — Contracts and server response semantics

## Summary

Bring Dockyard's contract-first and core server response surface into conformance
with MCP `2026-07-28`: JSON Schema 2020-12, unrestricted structured output,
cache metadata, and standard error behavior. This phase resolves any conflict
between the new schema target and V1's documented recursive-contract limit.

## RFC anchor

- RFC §5
- RFC §6
- RFC §16
- RFC §19.1

## Briefs informing this phase

- brief 07
- brief 06
- brief 03

## Brief findings incorporated

- **brief 07 §1.4:** full JSON Schema 2020-12, cache semantics, and error codes
  are protocol obligations, not optional HTTP polish.
- **brief 06 §2:** Go contracts remain the source of truth; downstream schemas
  are generated, never hand-authored.
- **brief 03 §2.2:** the SDK supplies server primitives but Dockyard owns its
  contract-first mapping and validation policy.

## Findings I'm departing from (if any)

- **D-052:** V1's recursive-contract limitation is revisited because full
  JSON Schema 2020-12 compatibility cannot silently retain it. The chosen outcome
  requires a superseding decision and generated-output goldens.

## Goals

- Emit and validate the schema capabilities MCP `2026-07-28` requires.
- Keep one Go-first contract source while safely supporting the expanded output
  domain and server cache/error semantics.

## Non-goals

- Hand-authored schemas or TypeScript types.
- External `$ref` dereferencing.
- OAuth policy or client behavior.

## Acceptance criteria

- [ ] Generated tool schemas are valid JSON Schema 2020-12 and golden-tested for
      composition, references, enums, embedded structs, and recursive contracts.
- [ ] Validation bounds schema depth/time and never dereferences external `$ref`.
- [ ] Structured tool content can carry every JSON value the new protocol permits
      without weakening typed Go handler contracts.
- [ ] Missing resources return the current standard JSON-RPC error code in modern
      mode while legacy behavior remains versioned.
- [ ] List/resource responses expose typed cache lifetime and scope metadata where
      the protocol permits it.
- [ ] `dockyard generate`, `validate`, and `test` reject stale or nonconformant
      generated output under the new schema dialect.

## Files added or changed

- `internal/codegen/*`, goldens, fuzz tests
- `runtime/tool/*`, `runtime/server/resource.go`, response/error tests
- `internal/protocolcodec/*` for versioned response metadata only
- `internal/validate/*`, `internal/testgate/*`
- `docs/specifications/*`, `docs/decisions.md`, `docs/glossary.md`
- `docs/site/`, `skills/define-contracts/SKILL.md`, `skills/add-a-tool/SKILL.md`
- `docs/plans/phase-34-contracts-server-semantics.md`
- `scripts/smoke/phase-34.sh`

## Public API surface

```go
type CachePolicy struct {
	TTL time.Duration
	Scope CacheScope
}
```

Any cache policy is a typed Dockyard surface; raw response wire objects remain
inside the versioned codec.

## Test plan

- **Unit:** schema dialect/output mapping, cache validation, error mapping.
- **Integration:** modern tools/resources round-trip through a real SDK transport
  and `dockyard generate`/`validate` fixture project.
- **Concurrency / golden:** codegen goldens, fuzzed schemas, and bounded validation
  tests; concurrent generator reuse where artifacts are reusable.

## Smoke script additions

- Assert 2020-12 goldens, recursive-contract resolution test, and modern resource
  semantics integration test exist.

## Coverage target

- `internal/codegen`: 80% new-package band or higher.
- `runtime/tool`, `runtime/server`: 85% conformance band.
- `internal/validate`: 75% CLI/tooling band.

## Dependencies

- 31 — vendored spec foundation.
- 32 — modern HTTP mode.
- 04, 05, 08 — codegen and handler runtime.

## Risks / open questions

- The pinned schema engine may not produce every required 2020-12 construct;
  replacing or extending it requires a decision, not an unreviewed post-process.
- Cache scope may require an explicit principal-aware safety rule in Phase 36.

## Glossary additions

- Cache policy.
- Cache scope.

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
