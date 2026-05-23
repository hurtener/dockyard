# Phase 18 — `dockyard generate` + `dockyard validate`

## Summary

Phase 18 lands the two contract-first quality verbs of the `dockyard` CLI.
`dockyard generate` runs the Design A codegen pipeline (RFC §6.2) over a
project's Go contracts to produce JSON Schema and `contracts.ts`, idempotently.
`dockyard validate` is the quality-gate command (RFC §9.4): it checks the
manifest, the generated schemas, tool↔UI mappings, MIME, spec compliance, UI
states, and stale-codegen drift, and exits non-zero on each build-blocker class.
The validation logic is a reusable internal package so `dockyard build` (20)
and `dockyard test` (21) call the same gate.

## RFC anchor

- RFC §6.1 — single source of truth (the Go struct).
- RFC §6.2 — the Design A pipeline; drift cross-check; stale generated output is
  a build blocker, not a warning.
- RFC §9.1 — the CLI command surface (`generate`, `validate`).
- RFC §9.4 — quality gates: the build-blocker / required-default / warning
  taxonomy `validate` honours.

## Briefs informing this phase

- brief 06
- brief 01
- brief 02

## Brief findings incorporated

- **Brief 06 §3.1 (Design A, R1).** Schema and TypeScript are generated
  independently from Go by two pure-Go tools, with no Node dependency, and a
  drift cross-check makes a desync a hard failure. `generate` drives exactly
  this pipeline through `internal/codegen`; `validate` invokes
  `codegen.CrossCheck` / `codegen.CheckStale`. Stale generated output is a
  build blocker.
- **Brief 06 §2.5.** The CLI is `spf13/cobra`. Each verb is one
  `func newXxxCmd() *cobra.Command` constructor plus one `root.AddCommand` line
  — the Phase 17 extension contract, followed verbatim.
- **Brief 01 §5.** The MCP Apps quality bar: a `ui://` resource must exist for
  every tool that declares one, MIME must be the single MVP type, and Apps
  constructs are validated against the vendored spec — not a live host. `validate`
  enforces these as build blockers.
- **Brief 02 §4.6.** Tasks lifecycle limits (max/default TTL, concurrency caps)
  are manifest-tunable and a default TTL above the max TTL is a manifest
  mistake — already structurally validated by `internal/manifest`; `validate`
  surfaces it in the manifest build-blocker class.

## Findings I'm departing from (if any)

None. The phase composes the existing `internal/codegen` and `internal/manifest`
surfaces; no brief finding is overridden.

## Goals

- `dockyard generate` regenerates a project's JSON Schema and `contracts.ts`
  from its Go contract structs, idempotently (a no-source-change rerun is a
  byte-identical no-op).
- `dockyard validate` runs every RFC §9.4 build-blocker check and exits
  non-zero when any fails; warnings are reported without failing the exit code.
- The validation logic is a reusable internal package (`internal/validate`)
  with a structured `Report`, so phases 20 and 21 wrap the same function.
- `generate` and `validate` are smoke-covered and integration-tested end to end.

## Non-goals

- `dockyard dev` (Phase 19), `build`/`run`/`install` (Phase 20), `test`
  (Phase 21). Phase 18 only keeps the cobra tree cleanly extensible and exposes
  `internal/validate` as the seam phases 20/21 consume.
- Live-host spec compliance. Compliance is checked against the vendored specs in
  `docs/specifications/` only (CLAUDE.md §11).
- Re-implementing codegen. `generate` is a CLI driver over `internal/codegen`.

## Acceptance criteria

- [x] `dockyard generate` is idempotent: a second run with no source change
      produces byte-identical files and reports no change.
- [x] `dockyard validate` exits non-zero on each build-blocker class — an
      invalid manifest, a broken tool↔UI mapping, an invalid MIME, a
      spec-compliance violation, stale generated output.
- [x] Stale generated output (a contract struct changed without rerunning
      `generate`) fails `validate`.
- [x] `dockyard validate` exits 0 on a clean scaffolded project.
- [x] Both verbs are registered on the cobra root and shown in `--help`.

## Files added or changed

- `internal/generate/` — the reusable generate engine (new package).
  - `generate.go` — `Run`, the schema + TypeScript regeneration driver.
  - `doc.go`
  - `generate_test.go` — golden + idempotency unit tests.
- `internal/validate/` — the reusable validation engine (new package).
  - `validate.go` — `Run`, `Report`, `Diagnostic`, severity taxonomy.
  - `checks.go` — the per-class checks (manifest, schema, mapping, MIME, spec,
    UI states, stale codegen).
  - `doc.go`
  - `validate_test.go` — table-driven per-class unit tests.
- `internal/cli/generate.go` — the `dockyard generate` cobra constructor.
- `internal/cli/validate.go` — the `dockyard validate` cobra constructor.
- `internal/cli/root.go` — two `root.AddCommand` lines.
- `internal/cli/doc.go` — verb-roadmap note updated (generate/validate landed).
- `runtime/tool/schema.go` — `MarshalSchema`, the public deterministic schema
  marshaller the in-project ephemeral generator uses (re-exports
  `codegen.Marshal`).
- `scripts/smoke/phase-18.sh` — the phase smoke script.
- `test/integration/generate_validate_test.go` — the end-to-end integration
  test.
- `docs/plans/phase-18-generate-validate.md` — this plan.
- `docs/decisions.md` — D-081, D-082, D-083.
- `docs/glossary.md` — `generate`, `validate`, build blocker, the ephemeral
  generator.

## Public API surface

- `internal/generate.Run(opts generate.Options) (generate.Result, error)` —
  consumed by `dockyard generate` and, later, `dockyard dev`.
- `internal/validate.Run(opts validate.Options) (*validate.Report, error)` —
  consumed by `dockyard validate` and, later, `dockyard build` / `dockyard
  test`. `Report.HasBlockers()` is the exit-code seam.
- `runtime/tool.MarshalSchema(*jsonschema.Schema) ([]byte, error)` — public,
  deterministic schema marshaller.

## Test plan

- **Unit:** `internal/generate` — golden tests pinning generated schema + TS for
  a fixed contract input; an idempotency test (generate twice → byte-identical).
  `internal/validate` — table-driven, one case per build-blocker class plus the
  clean-pass case; `Report` severity classification.
- **Integration:** `test/integration/generate_validate_test.go` — `dockyard
  new` a real project, `dockyard generate` it, assert a second `generate` is a
  byte-identical no-op, `dockyard validate` it (exit 0), then mutate the project
  to drive each build-blocker class (invalid manifest, broken tool↔UI mapping,
  stale generated output) and assert a non-zero exit each. Real files, real
  `codegen` / `manifest` drivers, no mocks. `-race`.
- **Concurrency / golden:** golden tests cover codegen determinism. `generate`
  and `validate` build fresh state per call and hold no shared mutable state, so
  no concurrent-reuse test is required (documented in the plan).

## Smoke script additions

`scripts/smoke/phase-18.sh` — one assertion per acceptance criterion:

- `dockyard --help` lists `generate` and `validate`.
- `dockyard generate` in a scaffolded project succeeds.
- a second `dockyard generate` produces no diff (idempotency).
- `dockyard validate` exits 0 on the clean scaffolded project.
- `dockyard validate` exits non-zero on an invalid manifest.
- `dockyard validate` exits non-zero on a broken tool↔UI mapping.
- `dockyard validate` exits non-zero on stale generated output.

A check against an unbuilt surface `skip()`s, never `fail()`s.

## Coverage target

- `internal/cli` — 70% (CLI/tooling default).
- `internal/generate` — 80% (new package).
- `internal/validate` — 80% (new package).
- `runtime/tool` — the existing target is unchanged; `MarshalSchema` is a thin
  re-export covered by the generate golden tests and the integration test.

## Dependencies

- Phase 17 — the cobra tree and `dockyard new` (the scaffolded project is the
  canonical input).
- Phase 05 — the codegen TypeScript generator and the `CrossCheck` / `CheckStale`
  drift detection.
- Phase 09 — the MCP Apps layer (`ui://` resources, MIME, tool↔UI mappings).
- Phase 13 — the Tasks extension contract surface (`task_support` coherence).

## Risks / open questions

- **External-module reflection.** The `dockyard` binary cannot reflect on a
  separate project's compiled contract types, and `internal/codegen` is not
  importable by a scaffolded project. `generate` therefore templates a small
  in-project ephemeral generator and `go run`s it for the schema half; the
  TypeScript half is pure source and runs in the CLI binary directly. See
  D-081.
- **`go run` cost.** The ephemeral generator costs one `go run` invocation per
  `generate`. Acceptable for a quality verb; `dockyard dev` (Phase 19) will
  decide its own caching posture.
- **Spec compliance scope.** V1 spec compliance is the mechanically-checkable
  subset against the vendored specs (MIME, ui:// URI shape, Apps/Tasks manifest
  constructs). Deep wire-level conformance is `dockyard test` (Phase 21). See
  D-083.

## Glossary additions

- **`dockyard generate`** — the contract-first codegen verb.
- **`dockyard validate`** — the quality-gate verb.
- **Build blocker** — a `validate` diagnostic class that forces a non-zero exit.
- **Ephemeral generator** — the templated in-project program `generate` `go
  run`s to produce schemas via reflection on the project's own contract types.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`

## Remediation notes

- **R3 / depth audit (D-113).** This plan states `validate` invokes
  `codegen.CrossCheck`. The depth audit found that was not so: `validate`'s
  `checkStaleCodegen` only byte-compared each artifact against a fresh
  regeneration; `CrossCheck` (the schema↔TS desync check) was wired only into
  `dockyard test`. Since `dockyard build` runs `validate`, not the test gate, an
  internally-inconsistent committed schema/TS pair passed both. R3 adds
  `checkCrossCodegen` to `internal/validate` — it reads the committed schema
  files and `contracts.ts` and runs `codegen.CrossCheck` per tool side,
  reporting a desync as a `CheckStaleCodegen` Blocker. `validate/doc.go`'s
  cross-check claim is now accurate. See D-113.

- **R4 N3 — D-113 wording clarification (depth-audit-2).** D-113 originally
  read "`dockyard validate` runs codegen.CrossCheck, so `dockyard build`
  catches schema↔TS desync". The depth-audit-2 follow-up found this
  overpromised the `build` path: `internal/buildpkg/build.go` runs
  `regenerateContracts` (stage 1) BEFORE invoking the validate gate (stage 2),
  so a hand-edited drifted `contracts.ts` is OVERWRITTEN by the regeneration
  step before `checkCrossCodegen` ever reads it. The build artifact still
  upholds P1, but via a different mechanism than `validate`-standalone — the
  build path "erases the drift", the validate-standalone and `dockyard test`
  paths "flag the drift". R4 N3 rewrites D-113 to distinguish the two paths
  explicitly, and tightens `internal/validate/doc.go` so the stale/cross-codegen
  bullets match the behaviour. No code changed.

- **R4 N4 — binary-boundary subprocess test for schema↔TS desync
  (depth-audit-2).** `internal/validate/run_test.go` covers
  `checkCrossCodegen` in-package; `dockyard test` and `dockyard build` tests
  reach the gate through their wrappers. No test drove the REAL `dockyard
  validate` binary against a project with a hand-edited
  `contracts.ts` desync. R4 N4 adds
  `test/integration/r4_validate_desync_test.go`
  (`TestR4_ValidateBinaryRejectsSchemaTSDesync`): it scaffolds a real project
  via the wave-7 `scaffoldWave7Project` helper, edits the committed
  `contracts.ts` to add a phantom `GreetOutput.extra` field absent from the
  JSON Schema, runs the real `dockyard validate` binary as a subprocess
  (`runCLI`), and asserts a non-zero exit with the cross-codegen Blocker
  fingerprint in the output. A future regression of the binary-boundary
  defense fails this test.
