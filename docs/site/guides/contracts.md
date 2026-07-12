# Contracts (Design A)

Dockyard is contract-first ([RFC §6](/reference/rfc)). A tool's typed Go
input and output structs are the source of truth. The JSON Schema the
host sees and the TypeScript types the App imports are both
**generated** by `dockyard generate` — never hand-written. The
`dockyard validate` quality gate catches a drift.

## Where contracts live

```text
internal/
  contracts/
    contracts.go          # the typed Go structs — the source of truth
    contracts.gen.json    # GENERATED — JSON Schema
web/
  src/
    generated/
      contracts.ts        # GENERATED — TypeScript types
```

Never edit a `*.gen.*` file. `dockyard validate` rejects hand-edits.

## A well-written contract

```go
// CreateMetricCardInput is the model-facing input for create_metric_card.
type CreateMetricCardInput struct {
    // Label is the metric's title (e.g. "Monthly Recurring Revenue").
    Label string `json:"label"`
    // Value is the headline numeric value.
    Value float64 `json:"value"`
    // Unit is an optional unit suffix ("USD", "ms", "%").
    Unit string `json:"unit,omitempty"`
    // Theme is the per-call theme override; "auto" honours the host.
    Theme ThemeMode `json:"theme,omitempty"`
}
```

- **Document every field.** Comments are lifted into the JSON Schema's
  `description` and the TypeScript JSDoc.
- **`omitempty` for optional fields.** Mark optional fields with
  `omitempty` so the codegen marks them optional in the schema.
- **Named scalars for constrained values.** A `ChartType` named type
  with documented allowed values teaches the model the right input.
- **The `Kind` discriminator pattern** — for outputs that drive a
  multi-renderer App, carry a string `Kind` so the App dispatches
  without sniffing the shape.

## Generate, validate, test

```bash
dockyard generate    # rewrite the generated artifacts (idempotent)
dockyard validate    # catches stale codegen + the CrossCheck drift
dockyard test        # the full contract + spec + capability gate
```

`dockyard build` runs `generate` + `validate` automatically — a stale
contract never ships.

## Schema dialect and references

Dockyard emits JSON Schema 2020-12 only. It retains the same
`google/jsonschema-go` inference engine used by the Go SDK and extends it for
recursive Go types. Recursive contracts generate deterministic local `$defs`
keys qualified by the Go package import path and local `$ref` back-edges.

Validation is deliberately local and bounded: external `$ref` and
`$dynamicRef` targets are rejected, as are schemas over 4 MiB, nesting beyond
128 levels, more than 100,000 schema nodes, or more than 1,000,000 units of
local-reference work. This prevents contract validation from becoming network
I/O or unbounded work.

## Output can be any JSON value

Tool inputs remain object-shaped, but a typed output may encode any JSON value:
an object, array, string, number, boolean, or null. Use the output type that
matches the value rather than wrapping a primitive solely to satisfy an old
object-only restriction.

A typed nil pointer, map, slice, or interface is omitted by default. To return a
present JSON null, set `StructuredPresent` explicitly:

```go
return tool.Result[*LookupOutput]{
    Structured:        nil,
    StructuredPresent: true,
}, nil
```

The runtime emits both `structuredContent: null` and the required JSON text
fallback. Leaving `StructuredPresent` false omits both.

## Why this matters

mcp-use, the closest competitor, has *types* but not *contracts* — the
widget's `useWidget<{...}>()` generic is hand-declared, and if the
server's output drifts the generic silently lies (brief
[04 §2.6](https://github.com/hurtener/dockyard/blob/main/docs/research/04-mcp-use-dx-teardown.md)).
Dockyard catches that drift at `dockyard validate`, before the binary
ships. P1 is the framework's headline. Lean on it.

## See also

- [`define-contracts` agent skill](/agent-skills/) — the same surface
  taught as an agent workflow.
- [Validate + test guide](validate) — the full quality-gate surface.
- [Decisions: D-113 — `dockyard validate` runs CrossCheck](/reference/decisions)
- [Decision D-193 — contract and response semantics](/reference/decisions)
