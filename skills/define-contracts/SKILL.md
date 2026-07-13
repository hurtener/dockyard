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
    contracts.go                 # typed Go structs — source of truth
    greet_input.schema.json      # GENERATED — per-tool input schema
    greet_output.schema.json     # GENERATED — per-tool output schema
    contracts.ts                 # GENERATED — App-facing TypeScript
.dockyard/
  generated-artifacts.json       # GENERATED — owned paths + SHA-256 digests
```

This location is required for manifest tool contracts, not merely a convention:
every `tools[].input` and `tools[].output` reference must use
`internal/contracts.<TypeName>`. That invariant keeps the single generated
`contracts.ts` complete for every App.

Two rules:

- **Never edit generated schemas, `contracts.ts`, or the ownership index** —
  `dockyard validate` fails on hand-edited or incomplete generated state
  (stale-codegen drift, RFC §6.2).
  The ownership index is byte-canonical, so unknown fields, record reordering,
  and formatting-only edits are rejected too.
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
- **Use `,string` only for scalar wire strings.** Booleans, strings, integers,
  floats, and one unnamed pointer to those scalar types are supported. Pointer
  fields stay nullable, and `omitempty` also makes them optional. Aggregate,
  interface, named-pointer, and deeper-pointer uses fail generation because
  `encoding/json` does not quote them.
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
- **Use recursive types when the domain is recursive.** Dockyard emits local
  package-qualified `$defs`/`$ref` graphs deterministically; do not flatten a
  tree merely to avoid recursion.
- **Let outputs match their JSON value.** Inputs remain object-shaped, but an
  output may be an object, array, scalar, or null.

## Generate

After any contract change:

```bash
dockyard generate
```

What happens:

- For every tool in `dockyard.app.yaml`, the codegen reads the Go input
  and output struct types named in `input:` / `output:` and produces:
  - `<tool>_input.schema.json` and `<tool>_output.schema.json` in the
    project's contracts package.
  - The combined TypeScript types in `internal/contracts/contracts.ts`.
  - `.dockyard/generated-artifacts.json`, which records each current
    generated path and its SHA-256 digest.
- Backend-only projects keep the same `internal/contracts/contracts.ts` output;
  UI-bearing projects import that canonical artifact rather than generating a
  second copy under `web/`.
- Obsolete generated files are removed conservatively. Dockyard only deletes an
  indexed path in a recognized generated namespace when its bytes still match
  the recorded digest and its generated marker is valid. It refuses to delete a
  modified artifact, follow a symlink, cross into a nested project, or act on an
  arbitrary indexed project file. Publication is rooted at the project
  directory, so a concurrent symlink swap cannot redirect a write outside it.
- Imported types are expanded to their JSON wire shape, including named
  declarations for recursive structs and the standard-library types supported
  by the schema engine. Canonical and imported types implementing
  `json.Marshaler` or `encoding.TextMarshaler` are generation errors unless
  Dockyard has an explicit wire-shape override; value and pointer receivers are
  both checked. Expose an explicit contract wire type instead.
  `math/big.Int`, `math/big.Rat`, and `math/big.Float` are rejected because
  their JSON shapes are addressability-dependent; use explicit integer or
  decimal string fields.
- The output is byte-deterministic: rerun with no source change ⇒ no
  diff (RFC §6.2 — idempotence is part of P1).
- JSON Schema output declares draft 2020-12. References are local-only;
  validation rejects external `$ref`/`$dynamicRef` and bounds schema size,
  depth, node count, and reference work.

If `dockyard generate` fails:

- **"unknown type ref"** — the manifest's `input:` / `output:` field
  doesn't resolve. Use `internal/contracts.<TypeName>` and confirm the exported
  Go type exists in that package.
- **"unsupported field type"** — Dockyard's codegen handles the common
  JSON-compatible Go types. Replace `time.Time` with `string` (ISO 8601)
  or `int64`, channels with serializable shapes, etc.

### Explicit null output

A typed nil pointer, map, slice, or interface is absent by default. Set
`StructuredPresent: true` when null itself is the intended result:

```go
return tool.Result[*LookupOutput]{
    StructuredPresent: true,
}, nil
```

This emits `structuredContent: null` and its JSON text fallback without
weakening the typed `Out` contract.

## Validate (the drift catcher)

```bash
dockyard validate
```

`dockyard validate` runs a battery of gates (RFC §9.4); the contract-
relevant ones:

- **Stale codegen** — the generated JSON Schema or TypeScript no longer
  matches the Go contract. Run `dockyard generate` to fix.
- **Stale ownership index** — a current generated path or digest is absent or
  stale, or an obsolete indexed artifact remains. Run `dockyard generate`; do
  not repair the index by hand.
- **`CrossCheck`** (D-113) — the JSON Schema and TypeScript that
  `dockyard generate` would produce *right now* match what's on disk.
  Catches a developer who edited a generated artifact by hand. `dockyard build`
  defends by regenerating before building.
- **Hand-edited generated files** — a generated file modified by hand is
  rejected; the standard fix is to revert and re-author the underlying Go
  contract.
- **Schema profile** — schemas must declare JSON Schema 2020-12, resolve using
  local references only, and stay within Dockyard's validation bounds. Input
  roots must be objects; output roots may be any JSON value.

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

- **Editing a generated schema, `contracts.ts`, or the ownership index.** Don't.
  Edit the Go struct and
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
