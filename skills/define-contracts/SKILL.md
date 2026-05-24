---
name: define-contracts
description: Define and evolve Dockyard tool contracts using Design A — the typed Go struct is the single source of truth; JSON Schema and TypeScript types are generated. Use when adding fields, changing types, or chasing a stale-codegen error. Covers `dockyard generate`, drift detection, and the CrossCheck a `validate` run does to catch silent server↔UI drift.
license: Apache-2.0
metadata:
  framework: dockyard
  surface: codegen
  verbs: "generate validate test"
---

# Define contracts the Dockyard way (Design A)

Dockyard is **contract-first**: the typed Go struct in your contracts
package is the source of truth for a tool's schema. The JSON Schema the
host sees and the TypeScript types your Svelte App imports are both
*generated* (RFC §6, Design A). You never hand-write either; if you try,
`dockyard validate` rejects it.

This skill covers: where contracts live, how to write them well, how
`dockyard generate` produces the artifacts, and how `dockyard validate`
catches a drift that would silently break the server↔UI contract.

## Where contracts live

The conventional location is `internal/contracts/`:

```text
internal/
  contracts/
    contracts.go          # the typed Go structs — the source of truth
    contracts.gen.json    # GENERATED — JSON Schema, one entry per tool
web/
  src/
    generated/
      contracts.ts        # GENERATED — TypeScript types for the App
```

Two rules:

- **Never edit `*.gen.*`** — `dockyard validate` fails on hand-edited
  generated files (stale-codegen drift, RFC §6.2).
- **Always run `dockyard generate` after a contract change** —
  `dockyard build` does it for you; in `dockyard dev` it runs
  automatically on a contract file change.

## Writing a contract well

A contract is a plain Go struct with `json:` tags and prose comments.

```go
// CreateMetricCardInput is the model-facing input for create_metric_card.
type CreateMetricCardInput struct {
    // Label is the metric's title (e.g. "Monthly Recurring Revenue").
    Label string `json:"label"`
    // Value is the headline numeric value.
    Value float64 `json:"value"`
    // Unit is an optional unit suffix ("USD", "ms", "%").
    Unit string `json:"unit,omitempty"`
    // Delta is the period-over-period change, signed.
    Delta float64 `json:"delta,omitempty"`
    // Series is an optional sparkline; one value per period.
    Series []float64 `json:"series,omitempty"`
    // Theme is the per-call theme override; "auto" honours the host.
    Theme ThemeMode `json:"theme,omitempty"`
}
```

The conventions that pay off downstream:

- **Document every field.** The codegen lifts the leading `//` comment into
  the JSON Schema's `description` and the TypeScript JSDoc. A field with no
  comment ships an empty `description` — a missed opportunity to teach
  the model.
- **Use `omitempty` for optional fields.** Read as "may be absent" by the
  codegen, which marks the field optional in the schema.
- **Prefer named scalar types for constrained values.** A `ChartType`
  named type with documented allowed values guides the model toward the
  right input; the App's TypeScript gets a typed union.
- **Use the `Kind` discriminator pattern** for outputs that drive a
  multi-renderer App. The `analytics-widgets` template's outputs each
  carry a string `Kind` field (`"chart"`, `"table"`, `"metric_card"`)
  so the App dispatches without sniffing the shape.
- **Mirror input → output** for renderer outputs. The App is a pure View
  — the model decides what to render; the App renders what arrives. So a
  `create_chart` output carries the input back through `Data` + `Options`
  plus the resolved `Theme`.

## Generate

After any contract change:

```bash
dockyard generate
```

What happens:

- For every tool in `dockyard.app.yaml`, the codegen reads the Go input
  and output struct types named in `input:` / `output:` and produces:
  - The JSON Schema (in the project's contracts package as a generated
    `.gen.json`).
  - The TypeScript types (in `web/src/generated/contracts.ts`).
- The output is byte-deterministic: rerun with no source change ⇒ no
  diff (RFC §6.2 — idempotence is part of P1).

If `dockyard generate` fails:

- **"unknown type ref"** — the manifest's `input:` / `output:` field
  doesn't resolve. Check the Go package path matches your contracts
  package's import path.
- **"unsupported field type"** — Dockyard's codegen handles the common
  JSON-compatible Go types. Replace `time.Time` with `string` (ISO 8601)
  or `int64`, channels with serializable shapes, etc.

## Validate (the drift catcher)

```bash
dockyard validate
```

`dockyard validate` runs a battery of gates (RFC §9.4); the contract-
relevant ones:

- **Stale codegen** — the generated JSON Schema or TypeScript no longer
  matches the Go contract. Run `dockyard generate` to fix.
- **`CrossCheck`** (D-113) — the JSON Schema and TypeScript that
  `dockyard generate` would produce *right now* match what's on disk.
  Catches a developer who edited `*.gen.*` by hand. `dockyard build`
  defends by regenerating before building.
- **Hand-edited generated files** — a generated file modified by hand is
  rejected; the standard fix is to revert and re-author the underlying Go
  contract.

A clean validate report ends with `0 blockers`. A blocker fails the
process exit code; warnings do not.

## Test gate

`dockyard test` runs all gates including the contract category — the
generated artifacts match the Go contract structs, the goldens are
coherent, and a spec-compliance check passes against the vendored MCP
specs. A contract regression fails the build.

```bash
dockyard test
```

## Why this matters

mcp-use, the closest competitor, has *types* (Zod inference on the server,
hand-declared generics on the widget) but not *contracts*: if the server's
output shape drifts, the widget's `useWidget<{...}>()` generic silently
lies (brief 04 §2.6). Dockyard catches that drift at `dockyard validate`
— before the binary ships. P1 is the framework's headline. Lean on it.

## Common pitfalls

- **Editing a `*.gen.*` file.** Don't. Edit the Go struct and
  regenerate.
- **Forgetting `omitempty` on an optional field.** The schema then
  requires the field, and a host that omits it gets a validation error
  from the runtime before your handler runs.
- **Adding a new tool to the manifest but not running `generate`.**
  `dockyard validate` will tell you; `dockyard build` auto-regenerates,
  so a CI-driven flow rarely surfaces this in practice.
- **Removing a field that the App still reads.** Run `dockyard validate`
  — the App's TypeScript build will fail with a clear compile error
  pointing at the deleted field.

## What to do next

- Wire the contract through to a UI ⇒ `attach-a-ui-resource` skill.
- Run the test gate end-to-end ⇒ `validate` skill (which covers
  the broader `dockyard validate` + `dockyard test` surface).
- Iterate quickly ⇒ `run-the-dev-loop` skill (the loop regenerates
  on contract changes automatically).
