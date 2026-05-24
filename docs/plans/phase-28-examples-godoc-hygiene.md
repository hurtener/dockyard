# Phase 28 — Examples, godoc, docs hygiene

## Summary

Polish the developer-meets-Dockyard surface ahead of the V1 cut: three
worked examples covering patterns the two shipped templates don't, a
godoc audit of `runtime/*` with runnable Example functions on the
highest-leverage APIs, a hygiene pass through the docs site + skills
landed in Phase 29, and a §19 drift-audit extension that mechanically
enforces every example has a README + a docs-site reference.

## RFC anchor

- RFC §2 — the binding product scope (server-side only, contract-first
  tools, MCP Apps, MCP Tasks, observability) this phase's examples
  exercise end-to-end.
- RFC §6 — the contract-first guarantee the examples adhere to (P1).
  The phase also adds `runtime/server.AddPrompt` as a focused
  pass-through for MCP Prompts, with the explicit constraint that
  contract-first does NOT extend to prompts (D-152).
- RFC §11 — the obs/v1 protocol the new `prompt.get` lifecycle hooks
  into via `obs.KindPromptGet` + `Recorder.PromptGet`.

## Briefs informing this phase

- brief 04 — DX teardown (mcp-use): the docs-must-stay-in-sync rule
  AGENTS.md §19 mechanically enforces; this phase tightens the
  enforcement around `examples/`.

## Brief findings incorporated

- **brief 04 §2.5** — "mcp-use's inspector is interactive but not a
  test harness." The Phase 28 worked examples reinforce the
  distinction by giving the inspector real, exercisable surfaces
  beyond the two templates — three concrete projects, one per
  developer-shaped use case (backend-only / composition / prompts).
- **brief 04 §3** — docs drift is the dominant defect class once a
  framework has shipped. The §19 hook is extended to examples here so
  a new example without a README or a docs-site reference fails
  drift-audit, the same shape the templates rule already has.

## Findings I'm departing from (if any)

None.

## Goals

- Ship three worked examples under `examples/` that build, validate,
  and run end-to-end against the current runtime, covering patterns
  the two templates don't:
  - `backend-tools-only` — pure-tools MCP server (no UI).
  - `combined-patterns` — analytics-widgets + approval-flows composed
    on one App.
  - `prompts-demo` — MCP Prompts via the new `runtime/server.AddPrompt`.
- Land the minimal `runtime/server.AddPrompt` API plus its obs/v1
  carrier (`obs.PromptGetPayload`, `Recorder.PromptGet`) and panic
  recovery, so the `prompts-demo` example is a real reference, not a
  TODO.
- Godoc pass on `runtime/*`: fill any gaps; add runnable `Example` test
  functions on the highest-leverage APIs (`runtime/server.New`,
  `runtime/server.AddPrompt`, `runtime/tool.New`) so pkg.go.dev renders
  copy-pasteable usage.
- Docs hygiene: re-cast every "read-only" inspector mention in the
  docs site + skills + the CLI source to the D-144 framing
  ("operator-initiated only"); add the D-139 pre-publish workflow
  (`go mod tidy` + `dockyard generate`) to the templates' READMEs
  (the docs + the scaffold-a-server skill already had it).
- Add a `docs/site/getting-started/examples.md` index page covering
  the three examples + the templates-vs-examples distinction.
- Extend `scripts/drift-audit.sh`'s §19 hook so every shipped example
  must have a README and must appear in the examples index page.

## Non-goals

- Adding the `examples/` tree to the `dockyard` binary as scaffold
  templates. Examples are reference projects developers read +
  manually clone, never `dockyard new --template` entry points
  (D-150).
- A contract-first prompts builder. Prompts in MCP carry a flat
  string-keyed argument map; the typed Go struct → JSON Schema
  pipeline does not extend naturally (D-152). `AddPrompt` is a
  focused, registration-only pass-through.
- Re-implementing the approval-flow engine for the
  `combined-patterns` example. It composes the same `tasks.Engine`
  the `approval-flows` template uses.
- Touching `examples/customer-health/`: that directory is the RFC
  §4.2 manifest reference fixture, consumed by
  `test/integration/wave2_test.go`. The drift-audit hook recognises
  the absence of a `cmd/server/` subdir as the "not a buildable
  example" exemption.

## Acceptance criteria

- [x] Three examples ship under `examples/<slug>/`:
      `backend-tools-only`, `combined-patterns`, `prompts-demo`.
- [x] Each example builds (`go build ./examples/<slug>/...`),
      validates (`dockyard validate` against its manifest), and runs
      (`go run ./examples/<slug>/cmd/server`).
- [x] Each example's handlers ship contract tests that pass under
      `-race`; coverage on each `internal/handlers` package meets its
      configured band (`coverage.json`).
- [x] `runtime/server.AddPrompt` exists; tested by an in-memory
      round-trip + panic recovery + concurrent reuse; emits an obs/v1
      `prompt.get` start+end pair.
- [x] `runtime/server/example_test.go` + `runtime/tool/example_test.go`
      run as pkg.go.dev `Example` blocks.
- [x] Every docs page + skill that says "the inspector is read-only"
      is updated to the D-144 framing.
- [x] Templates' READMEs document the D-139 pre-publish workflow
      (`go mod tidy` + `dockyard generate`).
- [x] `docs/site/getting-started/examples.md` exists, is wired into
      the VitePress sidebar, and links to each example.
- [x] `scripts/drift-audit.sh`'s §19 hook fires for an example
      without a README and for an example missing from
      `docs/site/getting-started/examples.md`.
- [x] `make drift-audit`, the full coverage gate, `go vet`,
      `golangci-lint`, `make build`, `make web`, `make docs`,
      `markdownlint`, `make check-mirror`, `make preflight` all
      pass.

## Files added or changed

- `examples/backend-tools-only/` — new: manifest, cmd/server/main.go,
  internal/{contracts,handlers}, README.md.
- `examples/combined-patterns/` — new: manifest, cmd/server/{main.go,
  index.html}, internal/{contracts,handlers}, README.md.
- `examples/prompts-demo/` — new: manifest, cmd/server/main.go,
  internal/{contracts,handlers}, README.md.
- `runtime/server/prompt.go` — new: AddPrompt + typed PromptDef /
  PromptRequest / PromptResult / PromptHandler + helpers.
- `runtime/server/prompt_test.go` — new: validation, round-trip,
  panic recovery, concurrent reuse tests.
- `runtime/server/example_test.go` — new: ExampleNew + ExampleAddPrompt
  for pkg.go.dev.
- `runtime/server/server.go` — adds `prompts []string` field and
  Prompts() accessor.
- `runtime/server/logbridge.go` — adds withPromptRequestSession.
- `runtime/server/doc.go` — adds Phase 28 prompts paragraph.
- `runtime/obs/event.go` — already had KindPromptGet; no change.
- `runtime/obs/payload.go` — adds PromptGetPayload.
- `runtime/obs/recorder.go` — adds Recorder.PromptGet.
- `runtime/tool/example_test.go` — new: ExampleNew for pkg.go.dev.
- `internal/cli/inspect.go` — re-casts the "read-only" docstring +
  Long help into the D-144 "operator-initiated only" framing.
- `docs/site/cli/index.md` — regenerated from inspect.go via
  `internal/clidocs`.
- `docs/site/guides/inspector.md`,
  `docs/site/guides/ui-resources.md`,
  `docs/site/getting-started/index.md` — D-144 framing updates.
- `docs/site/getting-started/examples.md` — new examples index.
- `docs/site/.vitepress/config.ts` — sidebar entry for examples.md.
- `skills/test-with-the-inspector/SKILL.md`,
  `skills/attach-a-ui-resource/SKILL.md` — D-144 framing updates.
- `templates/analytics-widgets/README.md.tmpl`,
  `templates/approval-flows/README.md.tmpl` — D-139 pre-publish
  workflow.
- `scripts/drift-audit.sh` — §19 hook extension for `examples/`.
- `scripts/smoke/phase-28.sh` — new: per-acceptance-criterion checks.
- `internal/coveragecheck/coverage.json` — entries for the three
  example handler packages + exempts for cmd/server +
  internal/contracts.
- `docs/decisions.md` — D-150 … D-153.
- `docs/plans/README.md` — Phase 28 status updated to Done.
- `docs/plans/phase-28-examples-godoc-hygiene.md` — this plan.

## Public API surface

`runtime/server` gains:

```go
func AddPrompt(s *Server, def PromptDef, fn PromptHandler) error
func (s *Server) Prompts() []string

type PromptArgument struct { Name, Title, Description string; Required bool }
type PromptDef       struct { Name, Title, Description string; Arguments []PromptArgument }
type PromptRequest   struct { Name string; Arguments map[string]string }
type PromptMessage   struct { Role, Text string }
type PromptResult    struct { Description string; Messages []PromptMessage }
type PromptHandler   func(ctx context.Context, req PromptRequest) (PromptResult, error)
```

`runtime/obs` gains:

```go
type PromptGetPayload struct { Prompt string; Messages, Bytes int }
func (r *Recorder) PromptGet(ctx context.Context, sc SpanContext, prompt string) func(messages, bytes int, err error)
```

## Test plan

- **Unit:** `runtime/server/prompt_test.go` (registration validation,
  in-memory round-trip, error propagation, panic recovery,
  concurrent reuse). Per-example handler unit tests.
- **Integration:** `examples/combined-patterns/internal/handlers/`
  drives a live `tasks.Engine` end-to-end (approve / reject /
  decline replies) so the goroutine-only helpers are covered.
- **Concurrency / golden:** `runtime/server/prompt_test.go`
  TestAddPrompt_Concurrent runs 8 sessions through `ServeInMemory`
  in parallel — the reusable-artifact rule under `-race`.

## Smoke script additions

- The `runtime/server.AddPrompt` symbol exists.
- The `obs.KindPromptGet` constant exists.
- Each example's manifest exists and parses (`dockyard validate`).
- Each example's handlers package compiles + tests pass.
- `runtime/server` has at least one Example function.
- `runtime/tool` has at least one Example function.
- The §19 examples hook is in `scripts/drift-audit.sh`.
- The CLI source no longer carries an unconditional "read-only"
  inspector framing.
- The templates' READMEs mention `go mod tidy` (D-139).
- The examples index page exists at
  `docs/site/getting-started/examples.md`.

## Coverage target

- `examples/backend-tools-only/internal/handlers`: 75% (cli-tooling).
- `examples/combined-patterns/internal/handlers`: 80% (new-package).
- `examples/prompts-demo/internal/handlers`: 75% (cli-tooling).
- Each example's `cmd/server` and `internal/contracts`: exempt.
- `runtime/server`: unchanged 85% (conformance) — the new
  `prompt.go` is covered by `prompt_test.go`.

## Dependencies

- 01 — 27.

## Risks / open questions

- **Risk:** the `prompts-demo` example exercises a runtime API that
  is brand-new in this phase, so it doubles as the integration test
  for `AddPrompt`. Mitigation: a dedicated `prompt_test.go` covers
  every registration-side branch (validation, round-trip, panic,
  concurrency) so the example can fail without obscuring the
  runtime API tests.
- **Open question:** the inspector's Tools panel does not render
  Prompts (Phase 23 scope was tools + resources + Tasks). The
  `prompts-demo` README documents this gap and points at a
  Prompts-aware host (Claude, an MCP CLI) for the visible demo.
  Adding a Prompts panel to the inspector is a post-V1 candidate.

## Glossary additions

None — the prompt + example vocabulary already exists in
`docs/glossary.md` via the RFC, MCP spec, and prior phases.

## Pre-merge checklist

- [x] `make drift-audit` passes (including the extended §19 examples
      hook).
- [x] `make check-mirror` passes.
- [x] `make preflight` passes (Phase 28 smoke `OK ≥ count(criteria)`,
      `FAIL = 0`).
- [x] `go test -race ./...` and `golangci-lint run` are clean.
- [x] All cross-references (`RFC §X.Y`, `brief NN`, `D-NNN`) resolve.
- [x] Coverage on touched packages ≥ stated targets.
- [x] The new `runtime/server.AddPrompt` public API has Example test
      coverage on pkg.go.dev AND a per-criterion smoke check.
- [x] Cross-subsystem seam consumed (Tasks engine in the
      combined-patterns integration tests): integration tests pass
      under `-race`.
- [x] New / changed architectural decisions filed
      (`D-150…D-153` in `docs/decisions.md`).
- [x] User-facing surface change ⇒ skills + docs updated in this PR
      (the D-144 / D-139 hygiene + the new examples + the new
      prompts API).
