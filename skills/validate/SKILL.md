---
name: validate
description: Run Dockyard's quality gates against a project with `dockyard validate` (build blockers) and `dockyard test` (the full contract + compliance + capability gate). Use before pushing, in CI, or to triage a build failure. Lists every check, the exit-code semantics, and how to interpret the report.
license: Apache-2.0
metadata:
  framework: dockyard
  surface: cli
  verbs: "validate test"
---

# Run Dockyard's quality gates

Two verbs share the quality-gate surface:

- **`dockyard validate`** — the fast, build-blocker check (manifest,
  schemas, tool↔UI mappings, App MIME, spec compliance against the
  vendored MCP specs, the four-state UI rule, stale-codegen drift).
  Used by `dockyard build` automatically and by CI to fail fast.
- **`dockyard test`** — the full contract + compliance gate (runs
  `go test`, the contract category, golden snapshots, spec compliance,
  capability degradation). Used by CI as a quality bar.

Both exit non-zero on a regression and 0 when clean. Warnings are
reported but do not change the exit code.

## `dockyard validate`

```bash
dockyard validate            # in the project directory
dockyard validate --dir path/to/project
```

The checks (RFC §9.4):

| Category           | What it catches                                             |
| ------------------ | ----------------------------------------------------------- |
| manifest           | malformed `dockyard.app.yaml`, missing required fields      |
| schemas            | the generated JSON Schema is not valid against draft-2020-12 |
| tool↔UI mappings  | a tool's `ui: <id>` does not match any `apps[].id`           |
| MIME               | an App's resource MIME is not `text/html;profile=mcp-app`   |
| spec compliance    | the Apps/Tasks shapes deviate from the vendored MCP specs   |
| four-state UI rule | a fixture is missing for a required UI state (§20)          |
| fixtures           | `require_fixtures` on + a UI-bearing tool ships no `fixtures/<tool>/*.json` (D-169) |
| contract tests     | `require_contract_tests` on + the project carries no `*_test.go` (D-168) |
| stale-codegen      | the generated `*.gen.*` files no longer match the Go source |
| CrossCheck (D-113) | the generated TS would differ from on-disk if regenerated   |

The `fixtures` and `contract tests` gates are opt-in via the manifest's
`quality.require_fixtures` / `quality.require_contract_tests` and enforced
from v1.3. `require_fixtures` is **UI-scoped** — it only requires fixtures
for tools that declare a `ui:` app (a non-UI server needs none).

A blocker exits non-zero (`validate: build blockers found`); a warning
reports inline but does not change the exit code.

Sample output:

```text
  blocker: tool "create_chart" references app "wigets" — no such app id
  warning: tool "summarise" has no fixture for the "permission" state
  validate: 1 blocker, 1 warning
```

Fix the blocker, re-run.

## `dockyard test`

```bash
dockyard test
dockyard test --skip-go-test   # the contract + spec + capability gates only
```

Categories (per the verb's `--help`):

| Category          | What it runs                                                  |
| ----------------- | ------------------------------------------------------------- |
| `go-test`         | the project's own `go test ./...`                             |
| `contract`        | the generated JSON Schema + TS still match the Go contracts   |
| `golden`          | the fixture / golden snapshots are present and coherent       |
| `spec-compliance` | the Apps/Tasks shapes conform to the vendored MCP specs       |
| `capability`      | the project degrades gracefully across host capability sets    |

A regression in any **gating** category exits non-zero. Warnings (e.g. a
contract test took longer than the soft budget) report inline but do not
fail the run.

`--skip-go-test` is the "the slow tests already ran in another CI step"
escape hatch — it keeps the contract / spec / capability gates running so
the framework's invariants are still enforced.

## Reading the report

`dockyard validate` and `dockyard test` both print in the same shape:
diagnostics first (blocker / failing categories), then warnings, then a
one-line verdict. The diagnostic format is `<level>: <message>` — no
stack traces, no cobra usage dump (the CLI suppresses cobra's
usage-on-error noise so a genuine failure is not buried).

When validate or test exits non-zero, the actionable detail is already
in the printed report — no extra debugging is needed.

## Common patterns

- **Run before pushing.** Add `dockyard validate && dockyard test` to a
  pre-push git hook.
- **Run in CI.** Both verbs are idempotent and produce deterministic
  output; pin them into the project's CI workflow.
- **Triage a `dockyard build` failure.** `build` runs `validate` first;
  if `build` fails with a `validate` blocker, run `validate` standalone
  for the focused, faster signal.
- **Catch contract drift in code review.** A PR that changes a contract
  but not the generated files should fail `validate` in CI. The fix is
  one `dockyard generate` away.

## Why this matters

mcp-use ships no `validate` or `test` verb at all (brief 04 §2.2). The
quality gate is unenforced; a developer can ship a server with a UI
that silently disagrees with the tool output and only see the
breakage in production. Dockyard's mandatory gates close that gap —
they are the framework's quality bar, codified.

## Common pitfalls

- **"Stale codegen" blocker.** Run `dockyard generate`. Re-run validate.
- **"Tool references unknown app".** Either fix the typo in the tool's
  `ui:` field or add the missing app to `apps[]`.
- **"Missing fixture for empty state".** Add a `fixtures/<tool>/empty.json`
  with a payload that drives the empty state. The `analytics-widgets`
  template's `fixtures/` directory is the canonical example.
- **"Spec-compliance" diagnostic citing a wire-format deviation.** Open
  the cited spec section in `docs/specifications/` — the vendored snapshot
  is the source of truth for the wire format Dockyard enforces.

## What to do next

- Iterate live ⇒ `run-the-dev-loop` skill.
- Ship a binary ⇒ `package` skill.
- Drive the failing tool by hand ⇒ `test-with-the-inspector` skill.
