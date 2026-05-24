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
