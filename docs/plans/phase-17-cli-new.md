# Phase 17 — dockyard CLI skeleton + dockyard new

## Summary

Phase 17 produces the `dockyard` binary users run: the `cmd/dockyard/`
entrypoint, the `spf13/cobra` command tree in `internal/cli/`, and the
`dockyard new` no-template project scaffold in `internal/scaffold/`. After this
phase `make build` actually builds `bin/dockyard` (it previously skipped). The
phase also folds in the Wave 6 checkpoint item **S1** — trace-correlating a
handler-emitted `obs/v1` log event to its enclosing `tool.call` span.

## RFC anchor

- RFC §9 — the CLI & developer experience (§9.1 one binary + command surface,
  §9.3 the settled CLI stack).
- RFC §10 — templates; Phase 17 builds the no-template (blank server) path,
  which RFC §10 names the first-class one.

## Briefs informing this phase

- brief 04
- brief 06

## Brief findings incorporated

- **brief 04 §3 — one binary, no Node.** "Dockyard ships one statically-linked
  CGo-free binary — no npx, no package fan-out, no Node on any install target."
  `cmd/dockyard` is that binary; `make build` pins `CGO_ENABLED=0`.
- **brief 04 §2 — DX-first ergonomics.** The teardown's bar is a tool that is
  productive from minute one. `dockyard new` produces a project that builds,
  tests, and serves immediately, with next-step guidance printed on success.
- **brief 06 §2.5 / §2 (CLI table) — `spf13/cobra` is the CLI stack.** "A
  multi-verb tool with subcommands, shell completions, gh/kubectl-familiar
  ergonomics." Adopted verbatim (also RFC §9.3, settled). The command tree is
  built so each later Wave 7 verb registers itself with one `AddCommand` line.
- **brief 06 (toolchain) — Go 1.26 pinned, `log/slog`.** The scaffolded
  `go.mod` pins `go 1.26.2`, matched to the framework; the CLI logs through a
  `log/slog` text handler, never `log.Printf`.

## Findings I'm departing from (if any)

None. One implementation decision the briefs did not anticipate is recorded as
a new decision: the scaffolded `go.mod` `replace` directive for the pre-release
workflow (D-080).

## Goals

- Ship the `dockyard` binary entrypoint — `cmd/dockyard/main.go` — so
  `make build` produces `bin/dockyard`, CGo-free and static.
- Ship the cobra command tree in `internal/cli/`, structured so phases 18–21
  add their verbs without restructuring it.
- Ship `dockyard new` — the no-template blank-MCP-server scaffold: a manifest,
  one example contract-first tool, generated contract artifacts, a runnable
  main, and a contract test. The output project builds and serves.
- Fold in the Wave 6 checkpoint item S1: a handler-emitted `obs/v1` log event
  is trace-correlated to its enclosing `tool.call` span.

## Non-goals

- `dockyard generate` / `validate` (Phase 18), `dev` (Phase 19), `build` /
  `run` / `install` (Phase 20), `test` (Phase 21). Phase 17 does not pre-stub
  them; each later phase adds its own command.
- Templates (`analytical-card`, `approval-flow`, `inspector`) — Wave 9. Phase
  17 builds only the no-template path.
- A scaffolded UI. The no-template server ships no UI, so it composes no
  `web/ui` inventory and the §20 four-state page rule does not apply here.

## Acceptance criteria

- [x] `dockyard new` produces a project that builds (`go build ./...`
      succeeds against the real runtime library).
- [x] That project serves — it boots as an MCP server; proven by its shipped
      contract test and by the integration test driving a real `tools/list` +
      `tools/call`.
- [x] The no-template path is first-class — `dockyard new <name>` works with no
      `--template` flag.
- [x] The scaffold's contract artifacts (JSON Schema, TypeScript) are generated
      from the Go contract structs, never hand-written (P1).
- [x] `make build` builds `bin/dockyard`, CGo-free.
- [x] S1: a handler-emitted `obs/v1` log event shares the trace id of its
      `tool.call` and nests under its span.

## Files added or changed

- `cmd/dockyard/main.go` — the binary entrypoint (replaces the Phase 01
  placeholder).
- `internal/cli/` — the cobra command tree:
  - `doc.go`, `root.go` — the root command + composition point.
  - `new.go` — the `dockyard new` verb.
  - `cli_test.go` — unit tests.
- `internal/scaffold/` — `dockyard new` project generation:
  - `doc.go`, `scaffold.go` — `Options`, `Generate`, validation.
  - `contracts.go` — the example tool's contract types + emitted source.
  - `templates.go` — the scaffolded project's file templates.
  - `scaffold_test.go`, `scaffold_golden_test.go` — unit + golden tests.
  - `testdata/golden/` — the golden scaffold fixture tree.
- `runtime/obs/recorder.go` — `WithSpan` / `SpanFromContext` /
  `ChildOrNewTrace` (the S1 context seam).
- `runtime/server/tool.go` — thread the `tool.call` span onto the handler ctx.
- `runtime/server/logbridge.go` — the log bridge reads the in-flight span.
- `runtime/obs/recorder_test.go`, `runtime/server/logbridge_trace_test.go` —
  S1 tests.
- `test/integration/phase17_scaffold_test.go` — the integration test.
- `scripts/smoke/phase-17.sh` — the smoke script.
- `go.mod` / `go.sum` — `spf13/cobra`.
- `docs/decisions.md`, `docs/glossary.md` — D-079, D-080; new vocabulary.

## Public API surface

- `internal/cli`: `NewRootCmd(stdout, stderr io.Writer) *cobra.Command`,
  `Execute(ctx context.Context) int`. Later phases extend the tree via
  `root.AddCommand`.
- `internal/scaffold`: `Generate(Options) (Result, error)`; `Options`
  (`Name`, `Dir`, `ModulePath`, `DockyardReplace`); sentinels `ErrInvalidName`,
  `ErrTargetExists`.
- `runtime/obs`: `WithSpan(ctx, SpanContext) context.Context`,
  `SpanFromContext(ctx) (SpanContext, bool)`, `ChildOrNewTrace(ctx) SpanContext`
  — the handler-span correlation seam other emit sites consume.

## Test plan

- **Unit:** `internal/cli` — root help lists `new`, bare invocation prints
  help, `new` scaffolds / rejects an invalid name / refuses an existing
  project / requires one arg / `--dockyard-path` adds a replace.
  `internal/scaffold` — name validation table, file-set production, non-empty
  vs empty target, determinism, module path + replace, contracts-are-generated.
  `runtime/obs` — `WithSpan` / `ChildOrNewTrace`.
- **Integration:** `test/integration/phase17_scaffold_test.go` — runs the real
  `dockyard new` scaffold, then `go mod tidy` + `go build` + `go vet` +
  `go test` it with the real toolchain (the "builds" proof); drives a real MCP
  `tools/list` + `tools/call` over a real in-memory transport against a server
  built from the scaffold's own contract types (the "serves" proof); covers a
  failure mode (a non-empty target). Runs under `-race`.
- **Concurrency / golden:** golden test pins every scaffolded file
  (`testdata/golden/`). The S1 correlation test runs under `-race`. The CLI and
  scaffold are not reusable concurrent artifacts (a `Generate` call and a
  command run are independent), so no concurrent-reuse test is owed; the
  `runtime/server` change is covered by the existing server concurrency tests.

## Smoke script additions

`scripts/smoke/phase-17.sh` asserts: the `dockyard` binary builds CGo-free; the
cobra root exposes `new`; `dockyard new` (no `--template`) produces a project;
the scaffold includes the manifest, main, and example tool; the scaffold ships
generated contract artifacts; the scaffolded project builds; its contract test
passes (it serves); and the S1 trace-correlation test passes.

## Coverage target

- `internal/cli` — 70% (CLI/tooling).
- `internal/scaffold` — 80% (new package).
- `runtime/obs`, `runtime/server` — the S1 change keeps these at their existing
  targets (≥85% / ≥80%).

## Dependencies

- Phase 06 (`internal/manifest`) — `dockyard new` writes a valid manifest. The
  scaffold also consumes `internal/codegen` (Phase 04/05) and the `runtime`
  library (Phases 01, 07, 08).

## Risks / open questions

- **Pre-release module resolution.** Dockyard is not published to a module
  registry, so a scaffolded project cannot `go get` the runtime library. The
  `--dockyard-path` flag adds a `go.mod` `replace` directive pointing at a
  local checkout (D-080). A released Dockyard drops the flag and the scaffold
  depends on the published module version. The flag is hidden so the released
  CLI surface is unaffected.
- **Golden brittleness.** The golden scaffold tree is pinned to the exact
  generated bytes; a deliberate template change requires `-update`. This is the
  intent — an accidental change fails CI.

## Glossary additions

- **Scaffold** — the project tree `dockyard new` generates.
- **No-template path** — `dockyard new` with no `--template`: the first-class
  blank-MCP-server scaffold (RFC §10).
- **Handler span** — the `obs/v1` `tool.call` span threaded onto a tool
  handler's context so a nested emit (a handler-emitted log event) correlates
  to it.

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
