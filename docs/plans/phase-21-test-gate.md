# Phase 21 — `dockyard test` — the contract + compliance gate

## Summary

Phase 21 ships `dockyard test` — the contract + compliance gate verb. It runs,
as one command against a Dockyard project, every test category Dockyard's
quality bar (RFC §9.4, G5) is built on: the project's own `go test`, the
contract-first assertions, the fixture/golden snapshots, MCP spec compliance
against the vendored specs, and capability-degradation tests across host
capability sets. A regression in any gating category exits non-zero. The phase
also folds in the Phase 20↔17 wiring-gap fix — the scaffolded server now honours
`DOCKYARD_TRANSPORT` so it can serve HTTP, not only stdio.

## RFC anchor

- RFC §9.1 — the one-binary command surface (`dockyard test` row).
- RFC §9.4 — Quality gates: the build-blocker / required-default / warning
  taxonomy `dockyard test` enforces.

## Briefs informing this phase

- brief 04 — the mcp-use DX teardown.
- brief 01 — the MCP Apps extension brief (capability degradation, RFC §7.5).

## Brief findings incorporated

- brief 04 §2.9 / §5: "No real test toolchain — no `test`/`validate` command,
  no fixtures, no golden snapshots, no host-compat tests, no quality gates.
  Quality is unenforced." `dockyard test` is the verb that closes this hole —
  it makes the contract tests, the golden snapshots, and the host-compat checks
  a single CI-enforceable gate, not a manual inspector poke.
- brief 04 §2.5: mcp-use's inspector "is an interactive inspect surface, not a
  test harness… you can poke the server by hand; you cannot encode 'this is
  correct' and have CI enforce it." Phase 21 deliberately makes `dockyard test`
  the *non-interactive, scriptable* harness — it runs headless, returns an exit
  code, and is the thing CI invokes.
- brief 04 §3 ("dockyard test — go test + contract tests + fixture/golden
  snapshots + host-compatibility matrix; the gate mcp-use lacks"): the category
  list is taken verbatim as the command's scope.
- brief 01 §4 (sharp edge 3) / §5: host support is read from capability
  negotiation, never a hardcoded host matrix. The capability-degradation
  category exercises the project across capability *sets* (Apps negotiated /
  not, a display mode supported / not) via the `runtime/apps` host-profile seam
  — it asserts graceful degradation, and it asserts no per-host matrix is
  consulted.

## Findings I'm departing from (if any)

None from the briefs. One **scope deviation** is recorded as a decision: Phase
21 folds in the Phase 20↔17 transport wiring-gap fix. The fix is self-contained
in the Phase 17 scaffold and the `DOCKYARD_TRANSPORT` environment-variable
contract. Phase 20 (`internal/runpkg`) has since landed on `main`; this branch
was rebased onto it, and the contract was verified against `runpkg` directly —
`runpkg` sets `DOCKYARD_TRANSPORT` (`stdio`/`http`) and `DOCKYARD_HTTP_ADDR` on
the server child, exactly the variables the scaffold reads. One mismatch was
found and fixed during the rebase: `runpkg`'s `defaultHTTPAddr` was `:8080`
(all interfaces), which would have silently widened the scaffold's secure
`127.0.0.1:8080` localhost default; `runpkg` now defaults to `127.0.0.1:8080`
to match (CLAUDE.md §17 cross-phase fix). See D-090 and the "Folded-in scope"
note under Goals.

## Goals

- `dockyard test` runs all five categories against a project as one command:
  `go test`, contract tests, fixture/golden snapshots, spec compliance, and
  capability-degradation tests.
- A gating regression in any category exits the process non-zero; informational
  output never changes the exit code (RFC §9.4 — the build-blocker vs warning
  distinction).
- The command composes the existing seams — `internal/validate.Run`,
  `internal/generate`, the `internal/codegen` golden machinery, the
  `runtime/apps` host-profile registry — it does not reimplement them.
- Output is clear and actionable: a per-category verdict and, on failure, the
  specific regression — the "DX better than mcp-use" bar (brief 04).
- **Folded-in scope — the Phase 20↔17 wiring-gap fix.** The Phase 17 scaffold's
  generated `main.go` now reads `DOCKYARD_TRANSPORT` (values `stdio` and `http`;
  default `stdio` when unset) and serves the selected transport via the
  `runtime/server` transport API. Previously it served stdio unconditionally —
  so `dockyard run --transport http` would set `DOCKYARD_TRANSPORT=http` and the
  server would ignore it. The env-var name is the contract Phase 20's
  `dockyard run` sets. Verified against `internal/runpkg` after the rebase onto
  Phase 20: `runpkg` sets `DOCKYARD_TRANSPORT` and `DOCKYARD_HTTP_ADDR` on the
  server child. `runpkg`'s `defaultHTTPAddr` was corrected from `:8080` to
  `127.0.0.1:8080` so a no-`--addr` HTTP run keeps the scaffold's localhost
  default (CLAUDE.md §17 cross-phase fix; `internal/cli/run.go` flag help
  updated to match).

## Non-goals

- `dockyard run` / `dockyard build` / `dockyard install` themselves (Phase 20).
  Phase 21 only fixes the *scaffold side* of the run-transport seam.
- The interactive inspector and its fixture switcher (Phase 22+, RFC §12).
  `dockyard test` is the headless, scriptable harness; the inspector is the
  interactive surface.
- A live-host compatibility matrix. Capability degradation is tested against the
  in-process host-profile seam and capability sets, never a live host
  (CLAUDE.md §11, §6).
- Re-running `dockyard validate`'s manifest/schema/MIME checks as a separate
  category: `dockyard test`'s spec-compliance category *reuses* `validate.Run`'s
  spec checks rather than duplicating them.

## Acceptance criteria

- [x] `dockyard test` is wired onto the cobra root and runs all five categories
      against a Dockyard project.
- [x] A contract regression (a contract struct changed without regenerating, so
      the generated schema/TS is stale) fails the run — exit non-zero.
- [x] A spec-compliance violation fails the run — exit non-zero.
- [x] A clean scaffolded project passes — exit zero.
- [x] The scaffolded server honours `DOCKYARD_TRANSPORT=http` and genuinely
      serves HTTP (the Phase 20↔17 wiring-gap fix).

## Files added or changed

```text
internal/cli/
  root.go                      # +1 root.AddCommand(newTestCmd())
  test.go                      # new — the `dockyard test` cobra command (thin wrapper)
internal/testgate/             # new package — the testable gate engine
  doc.go
  testgate.go                  # Run(Options) (*Report, error); Category, Result, Report
  categories.go                # the five category runners
  testgate_test.go             # unit tests — orchestration, Report, capability, helpers
  run_test.go                  # in-package run tests against a real scaffolded project
internal/cli/
  test_test.go                 # `dockyard test` cobra-command tests
runtime/apps/
  hostprofile.go               # +RegisteredHostIDs() — the host-profile read seam
internal/scaffold/
  templates.go                 # renderMainGo now honours DOCKYARD_TRANSPORT
  testdata/golden/main.go.golden  # regenerated golden
  scaffold_test.go             # +transport-honouring assertion
internal/runpkg/
  run.go                       # defaultHTTPAddr :8080 → 127.0.0.1:8080 (CLAUDE.md §17 cross-phase fix)
internal/cli/
  run.go                       # --addr flag-help default updated to 127.0.0.1:8080
test/integration/
  phase21_test_gate_test.go    # new — end-to-end integration test
scripts/smoke/
  phase-21.sh                  # new — one assertion per acceptance criterion
docs/plans/
  README.md                    # Phase 21 row → Shipped
  phase-21-test-gate.md         # this file
docs/decisions.md               # +D-089, +D-090
docs/glossary.md                # +test gate, +capability-degradation test, +DOCKYARD_TRANSPORT
```

## Public API surface

```go
// internal/testgate — the gate engine `dockyard test` wraps.
package testgate

type Options struct {
    ProjectDir string // project root (holds dockyard.app.yaml). Required.
    SkipGoTest bool   // skip the `go test` category (used by fast smoke runs).
}

type Category string // "go-test", "contract", "golden", "spec-compliance", "capability"

type Result struct {
    Category Category
    Passed   bool
    Gating   bool   // a failed gating category exits the process non-zero
    Detail   string // actionable, human-facing
}

type Report struct{ Results []Result }

func (r *Report) Failed() bool          // any gating category failed
func Run(opts Options) (*Report, error) // ErrTestGate on a run-fault, never a category fault
```

## Test plan

- **Unit (`internal/testgate`):** table-driven tests over a clean fixture
  project and over each gating regression — a stale contract, a missing
  vendored-spec proxy — asserting the right category fails and `Report.Failed()`
  flips. `Run` returns `ErrTestGate` (not a `Report`) only on a genuine run
  fault (missing project, manifest will not load). `-race`.
- **Unit (`internal/scaffold`):** the existing golden test pins the regenerated
  `main.go`; an added assertion proves the rendered `main.go` references
  `DOCKYARD_TRANSPORT` and the HTTP transport entrypoint.
- **Integration (`test/integration/phase21_test_gate_test.go`):** CLAUDE.md §17
  — Phase 21's Deps name Phase 18 and it consumes `internal/validate`,
  `internal/codegen`, `internal/generate` and `runtime/server`/`runtime/apps`.
  The test runs the real `dockyard new` scaffold, `go mod tidy`s it against the
  real Dockyard checkout, then: (a) runs `testgate.Run` on the clean project and
  asserts every category passes and the run exits clean; (b) introduces a
  contract regression and asserts the contract category fails; (c) introduces a
  spec-compliance violation and asserts the spec category fails; (d) ≥1 further
  failure mode — a project whose `go test` fails; (e) builds the scaffolded
  server and proves it honours `DOCKYARD_TRANSPORT=http` by issuing a real MCP
  initialize over HTTP. Real components, no mocks at the seam, `-race`.
- **Concurrency / golden:** `testgate.Run` builds fresh state per call and holds
  no shared mutable state — proven by a parallel-invocation test under `-race`.
  The scaffold golden test covers the changed `main.go`.

## Smoke script additions

`scripts/smoke/phase-21.sh` — one assertion per acceptance criterion:

- the cobra root exposes `test`;
- `dockyard test` runs all categories on a clean scaffolded project and exits 0;
- a contract regression makes `dockyard test` exit non-zero;
- a spec-compliance violation makes `dockyard test` exit non-zero;
- the scaffolded server honours `DOCKYARD_TRANSPORT=http` (the wiring-gap fix).

A check against unbuilt surface `skip()`s, never `fail()`s.

## Coverage target

- `internal/testgate` — 80% (new package).
- `internal/cli` (the `test.go` wrapper) — 70% (CLI/tooling).
- `internal/scaffold` — unchanged; the golden test covers the new `main.go`.

## Dependencies

- Phase 18 (`dockyard generate` + `dockyard validate` — `internal/validate.Run`,
  `internal/generate`, `internal/codegen`).
- Phase 17 (the scaffold — the wiring-gap fix touches it).
- Phase 09 / 12 (`runtime/apps` host profiles — the capability-degradation
  category).

## Risks / open questions

- **`go test` cost.** Running the project's full Go test suite is the slow
  category. `Options.SkipGoTest` lets a fast smoke run skip it; the integration
  test and a real `dockyard test` run it.
- **Phase 20 ordering — resolved on rebase.** This branch was authored before
  Phase 20 landed; the wiring-gap fix was folded in here (D-090) because it is
  self-contained in the scaffold and the `DOCKYARD_TRANSPORT` contract. The
  branch has since been rebased onto Phase 20, and the contract was verified
  directly against `internal/runpkg`: `runpkg` sets `DOCKYARD_TRANSPORT`
  (`stdio`/`http`) and `DOCKYARD_HTTP_ADDR` on the server child — the same
  variables the scaffold reads. `runpkg`'s `defaultHTTPAddr` was corrected from
  `:8080` to `127.0.0.1:8080` so the seam keeps the scaffold's secure localhost
  default end to end.

## Glossary additions

- **test gate** — the `dockyard test` command and its `internal/testgate`
  engine: the contract + compliance gate that runs all test categories.
- **capability-degradation test** — a `dockyard test` category that exercises a
  project across host capability sets and asserts graceful degradation.
- **`DOCKYARD_TRANSPORT`** — the environment variable a scaffolded Dockyard
  server reads to select its transport (`stdio` | `http`).

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
