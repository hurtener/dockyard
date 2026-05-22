# Phase 21.5 — test-quality hardening

## Summary

Phase 21.5 closes the four genuine gaps an independent test-quality audit found
in an otherwise above-average suite: coverage bands that were enforced only by
reviewer eye, no fuzz targets on the parse/decode surfaces, no benchmarks on the
hot reusable artifacts, and a handful of missing concurrency proofs and thin
edge-case coverage. It adds **mechanical** coverage gating (Go + frontend, CI-
enforced), Go native fuzz targets, benchmarks, and the missing `-race`
concurrency tests — no product or runtime behaviour changes.

## RFC anchor

- RFC §9.4 — Quality gates: "how the high minimum bar (G5) is enforced — by the
  toolchain, not documentation." Phase 21.5 applies that same principle to the
  AGENTS.md §11 coverage bands: it makes them a toolchain gate, not a prose
  aspiration.

## Briefs informing this phase

- brief 04 — the mcp-use DX teardown.
- brief 06 — the Go-2026 no-CGo stack & toolchain.

## Brief findings incorporated

- brief 04 §2.5 / §3: mcp-use "is an interactive inspect surface, not a test
  harness… you cannot encode 'this is correct' and have CI enforce it." The same
  insight applies inward: a coverage band the toolchain does not enforce is not
  a gate. Phase 21.5 makes the bands a CI-enforced gate (`make coverage`).
- brief 06 (Go-2026 toolchain): Go 1.26 ships native `FuzzXxx` fuzzing and
  `BenchmarkXxx` benchmarking in the standard `go test` toolchain — no third-
  party dependency. Phase 21.5 uses exactly those built-in mechanisms; the fuzz
  corpus runs as ordinary tests in CI (the default when `-fuzz` is absent).
- brief 06 (no-CGo discipline): the coverage run keeps `CGO_ENABLED=1` only for
  the `-race` detector, consistent with `make test`; benchmarks run `-race`-free
  so their numbers are real. The shipped binary stays CGo-free.

## Findings I'm departing from (if any)

None from the briefs. One **honest scoping note**: this phase's primary
informing source is not an RFC section or a brief — it is the AGENTS.md §11
coverage bands plus the independent test-quality audit. RFC §9.4 is the closest
anchor (the "toolchain, not documentation" principle), and briefs 04/06 inform
the *how*, but the work is hygiene hardening, not a new RFC-specified subsystem.
This is recorded as D-092.

The audit also made one **wrong** claim — "no web UI tests" — which was
investigated and rejected: `web/ui/src/__tests__/` and `web/bridge/src/__tests__/`
hold ~94 Vitest tests run by `make web`. Phase 21.5 therefore adds frontend
coverage *thresholds* and wires them into the gate; it does not add web tests
that already exist. Recorded as D-093.

## Goals

- Coverage is mechanically enforced: a per-package Go coverage gate and per-
  project frontend coverage thresholds, both wired into CI, both failing the
  build on a regression.
- Go native fuzz targets exist for the prime parse/decode surfaces and run their
  seed corpus as ordinary CI tests.
- Benchmarks exist for the hot reusable artifacts and compile + run on demand.
- The `LogBridge` / `internal/validate` / `internal/generate` concurrency-proof
  gaps are closed, and `internal/cli` edge-case coverage is raised, so every
  package meets its band with margin.

## Non-goals

- New web UI tests — they already exist (D-093); only frontend coverage
  thresholds are added.
- Splitting the long `wave5`/`wave6` integration files — the audit rated this
  Low; left as-is.
- Any product, runtime, or wire-format behaviour change. The one code change is
  a defensive `recover` in `codegen.TypeScriptForSource` (D-094) — a bug the new
  fuzz target found; a panic fix, not a behaviour change.
- Making benchmarks a CI gate — they are run on demand (`make bench`).

## Acceptance criteria

- [x] A per-package coverage checker (`internal/coveragecheck`) parses a Go
      coverage profile, compares each package to its band, and exits non-zero on
      a shortfall; it is wired into `make coverage` and the CI `go` job.
- [x] Frontend coverage runs with thresholds: `web/ui` and `web/bridge` run
      `vitest run --coverage` with per-project `coverage.thresholds`, wired so
      `make web` fails on a frontend coverage regression.
- [x] `make coverage` and the frontend coverage gate genuinely fail on a
      regression (verified against a synthetic shortfall).
- [x] Go `FuzzXxx` targets exist for `internal/protocolcodec` (wire decode),
      `internal/manifest` (the `dockyard.app.yaml` loader), `internal/codegen`
      (Go-source parsing), and the JSON-RPC tool-argument frame path
      (`runtime/tool`); each has a seed corpus and an asserted invariant, and
      the corpus runs as an ordinary CI test.
- [x] `BenchmarkXxx` benchmarks exist for the `runtime/obs` ring buffer +
      fan-out, the `internal/protocolcodec` codecs, and the `runtime/store`
      drivers (`inmem` + `sqlite`); they compile and run via `make bench`.
- [x] A concurrent-stress `-race` test exists for `LogBridge`, for
      `internal/validate`'s reusable `Run` surface, and for `internal/generate`'s
      reusable `Plan` surface.
- [x] Every Go package meets its coverage band (per the threshold config), with
      documented overrides where a band is genuinely unreachable hermetically.

## Files added or changed

```text
internal/coveragecheck/                       # NEW — the mechanical coverage gate
  doc.go
  coveragecheck.go                            # profile parse + threshold check
  coveragecheck_test.go
  coverage.json                               # per-package threshold config
  cmd/coveragecheck/main.go                   # the gate's CLI entry point
internal/protocolcodec/fuzz_test.go           # NEW — wire-decode fuzz targets
internal/protocolcodec/bench_test.go          # NEW — codec encode/decode benchmarks
internal/manifest/fuzz_test.go                # NEW — manifest-loader fuzz target
internal/codegen/fuzz_test.go                 # NEW — Go-source-parse fuzz targets
internal/codegen/typescript.go                # CHANGED — recover guard (D-094)
internal/codegen/typescript_test.go           # CHANGED — malformed-tag regression test
internal/codegen/testdata/fuzz/FuzzTypeScriptForSource/  # NEW — crasher seed
internal/generate/concurrency_test.go         # NEW — Plan concurrency proof
internal/validate/concurrency_test.go         # NEW — Run concurrency proof
internal/cli/print_test.go                    # NEW — CLI print-helper edge cases
runtime/tool/fuzz_test.go                     # NEW — arg-frame fuzz target
runtime/tool/export_test.go                   # CHANGED — testing.TB so a fuzz F can build a runtime
runtime/obs/bench_test.go                     # NEW — ring buffer / fan-out benchmarks
runtime/server/logbridge_concurrency_test.go  # NEW — LogBridge concurrency proof
runtime/store/storetest/benchmark.go          # NEW — shared Store benchmark suite
runtime/store/storetest/storetest_test.go     # CHANGED — benchmark-suite self-guard
runtime/store/inmem/bench_test.go             # NEW — inmem driver benchmark
runtime/store/sqlitestore/bench_test.go       # NEW — sqlite driver benchmark
runtime/tasks/capturewriter_test.go           # NEW — captureWriter edge-case test
Makefile                                      # CHANGED — `coverage` + `bench` targets
.github/workflows/ci.yml                      # CHANGED — coverage gate step
.gitignore                                    # CHANGED — keep coverage.json tracked
scripts/drift-audit.sh                        # CHANGED — phase-id regex allows a dotted suffix
web/ui/package.json                           # CHANGED — gate runs coverage
web/bridge/package.json                       # CHANGED — gate runs coverage
docs/plans/README.md                          # CHANGED — Phase 21.5 entry
docs/plans/phase-21.5-test-quality.md          # NEW — this plan
scripts/smoke/phase-21.5.sh                    # NEW — the smoke script
AGENTS.md / CLAUDE.md                          # CHANGED — §11 mechanical-gate note
docs/decisions.md                             # CHANGED — D-092..D-095
docs/glossary.md                              # CHANGED — coverage gate, fuzz target, …
```

## Public API surface

- `internal/coveragecheck` — `LoadConfig`, `ParseProfile`, `Check`, `Config`,
  `PackageThreshold`, `Report`, `Result`, `WriteReport`, sentinels
  `ErrShortfall` / `ErrUnconfigured`. Build-tooling only; no other phase depends
  on it at runtime.
- `runtime/store/storetest.RunBenchmarks(*testing.B, func() store.Store)` — the
  shared Store benchmark suite, mirroring `RunConformance`; a driver's `_test.go`
  calls it.
- No runtime / handler-facing API changes.

## Test plan

- **Unit:** `internal/coveragecheck` — profile parsing, config loading, the
  shortfall / unconfigured / exempt / missing-measurement branches, report
  rendering. `internal/cli` print-helper and `mapScaffoldError` edge cases.
  `runtime/tasks` `captureWriter`. `internal/codegen` malformed-struct-tag
  regression.
- **Integration:** N/A — Phase 21.5 ships no cross-subsystem seam; it hardens
  existing packages' tests. (The coverage gate consumes `go test`'s own
  profile output, not another subsystem's API.)
- **Concurrency / golden:** concurrency — `LogBridge` (concurrent log records
  fanning to MCP + obs under `-race`), `validate.Run` and `generate.Plan`
  (concurrent invocation, race-free + deterministic). Fuzz — `protocolcodec`,
  `manifest`, `codegen`, `runtime/tool` corpora run as ordinary `-race` tests.
  Benchmarks compile and run under `make bench`.

## Smoke script additions

`scripts/smoke/phase-21.5.sh` asserts: the coveragecheck package + cmd + config
exist; `make coverage` is wired to the checker and CI runs it; the checker exits
non-zero on a synthetic shortfall; `web/ui` + `web/bridge` have Vitest coverage
thresholds and their gate runs `--coverage`; a fuzz target file with a `FuzzXxx`
function exists for each named parse surface; benchmark files exist for the
named hot paths and `make bench` exists; the `LogBridge` / `validate` /
`generate` concurrency tests exist.

## Coverage target

Per-package targets are now the **machine-checked** thresholds in
`internal/coveragecheck/coverage.json`, keyed to the AGENTS.md §11 bands:

- New packages — 80%. `internal/coveragecheck` lands at 96.4%.
- Conformance-tested subsystems + Store drivers — 85%.
- CLI / tooling — 70% (the `internal/validate` engine keeps Phase 18's 75%).
- **Documented overrides:** `internal/buildpkg` / `runpkg` / `installpkg` —
  70%, `subprocess-override`: they orchestrate subprocesses with branches a
  hermetic suite cannot reach (Phase 20 documented this); held to the CLI band,
  not the new-package band. `runtime/store/storetest` /
  `runtime/tasks/taskstoretest` — 65%, `harness-override`: conformance *harness*
  packages whose statements are exercised when a driver runs the suite, so self-
  coverage sits below every product band by construction. Every override
  carries its reason in the config and in D-092.

## Dependencies

Phase 21.5 hardens the tests of already-shipped phases; it depends on their code
being present. Deps (the phases whose packages it touches): 02 (protocolcodec),
03 (store), 04/05 (codegen), 06 (manifest), 08 (runtime/tool), 15/16 (obs +
LogBridge), 17 (cli), 18 (validate/generate), 21 (testgate). It inserts at the
end of Wave 7, after Phase 21, before Wave 8.

## Risks / open questions

- **Coverage flakiness.** A package sitting exactly on its band could flap a
  build on benign churn. Mitigated: thresholds are set at the band while every
  package currently measures above it with margin (the thinnest, `runtime/tasks`,
  was raised from 85.5% to 86.1% by an added edge-case test).
- **Fuzz corpus growth.** `go test -fuzz` writes new corpus entries under
  `testdata/fuzz/`. Only deliberate regression seeds are committed (the
  `FuzzTypeScriptForSource` crasher). CI runs the committed corpus, never an
  unbounded `-fuzz` session.
- **Benchmark numbers are environment-dependent.** Benchmarks are a baseline +
  regression-spotting tool, never a CI gate — no risk of a CI flake.

## Glossary additions

- **Coverage gate** — the mechanical per-package coverage check
  (`internal/coveragecheck`) that fails the build on a band shortfall.
- **Fuzz target** — a Go `FuzzXxx` function exercising a parse/decode surface
  against arbitrary input with an asserted invariant.
- **Benchmark** — a Go `BenchmarkXxx` function measuring a hot reusable
  artifact; a baseline, not a gate.
- **Coverage band** — one of the three AGENTS.md §11 coverage tiers (new-package
  80%, conformance 85%, CLI-tooling 70%).

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
