# combined-patterns

A worked example showing the **analytics-widgets** and **approval-flows**
patterns COMPOSED in one Dockyard server, sharing one bundle and one
dispatcher across two MCP App resources (Phase 28, D-150). The two
shipped templates aren't isolated — they combine into a real product
flow: *insight → action*.

The domain is a **feature-flag rollout reviewer**: the agent surfaces
the current rollout's health (analytics widget), then proposes the next
action — advance / pause / rollback — for the user to approve (approval
flow). Both renderers live on the same App; the agent might call them in
sequence ("show me checkout-v2 rollout health" → "if it looks fine,
propose the next ramp") and the user sees the metric and the prompt in
the same chat surface.

## What it ships

- **`rollout_health`** — synchronous, returns a metric card with the
  current rollout's error rate, a sparkline trend, a tone (`ok` /
  `warn` / `critical`), and a suggested next action. Pure analytics-
  widget pattern.
- **`propose_rollout_action`** — task-supporting; pauses the task at
  `input_required` carrying an approval card (title + rationale +
  proposed action). The user approves or rejects through the bridge;
  the task resumes with the final decision.

The two tools target **two ui:// App resources** (`rollout_metric` for
the synchronous metric view, `rollout_approval` for the task-augmented
approval view) — RFC §8.6 requires every tool sharing a ui:// app to
agree on `task_support`, so a synchronous widget and a task-augmented
approval flow are different View contracts even when they sit in the
same product surface. Both apps point at **the same** `index.html`
bundle; the in-bundle dispatcher routes on `structuredContent.kind`
(`metric_card` → metric renderer; `approval` → approval renderer) so
one HTML serves both. This is the composition the example demonstrates
— one bundle + one dispatcher + two view contracts.

## Layout

```text
examples/combined-patterns/
├── dockyard.app.yaml                   # the manifest — 2 tools + 1 App
├── cmd/server/
│   ├── main.go                         # registers the App + tools, mounts the Tasks engine
│   └── index.html                      # the App: a small static HTML dispatcher (no Vite)
├── internal/contracts/contracts.go     # the typed contracts (analytics + approval halves)
└── internal/handlers/                  # the handlers + their tests
    ├── handlers.go
    └── handlers_test.go
```

**Why a hand-written `index.html` instead of a Vite-built Svelte App?**
The example's job is to show the **server-side composition** of the two
patterns — the dispatcher routing on `kind`, the Tasks engine wiring,
the contract pairs. A Vite build adds an `npm install` step that
distracts from the point. The `analytics-widgets` template ships the
full Vite + Svelte path; copy that pattern when you scaffold for real.

## Try it

```bash
# From the repo root.
cd examples/combined-patterns

# 1) Generate the schemas + TypeScript from the Go contracts.
dockyard generate

# 2) Validate the manifest + contracts.
dockyard validate

# 3) Run it (stdio is the default).
go run ./cmd/server

# 4) Or, run it over streamable-HTTP on 127.0.0.1:8080:
DOCKYARD_TRANSPORT=http go run ./cmd/server

# 5) Drive it under the inspector — call rollout_health to see the
#    metric card; then propose_rollout_action to see the approval card
#    in the same App.
DOCKYARD_TRANSPORT=http go run ./cmd/server &
dockyard inspect --url http://127.0.0.1:8080
```

Run the handler tests:

```bash
go test ./internal/handlers
```

## The composition pattern

The interesting part is the **shared bundle + shared dispatcher**
behind two view contracts. Three things make this work:

1. **Two ui:// apps, one bundle.** RFC §8.6 says tools sharing a
   ui:// app must agree on `task_support` — a synchronous tool and a
   task-supporting tool are different view contracts. The example
   ships two app entries (`rollout_metric`, `rollout_approval`) whose
   `entry` both point at the SAME `cmd/server/index.html`, so the
   bundle is loaded once and reused by both views.

2. **Both output contracts carry a `Kind` discriminator.** The
   `rollout_health` output uses `Kind: "metric_card"`; the
   `propose_rollout_action` output uses `Kind: "approval"`. The
   in-bundle `render()` switch reads `payload.structuredContent.kind`
   and picks the renderer — the exact pattern the templates use.

3. **The Tasks engine is attached to the server.** The
   `propose_rollout_action` tool has `task_support: required` in the
   manifest, so its handler must own a `tasks.Engine` to drive the
   input_required round-trip. The scaffolded `main.go` constructs a
   real engine over an in-memory `TaskStore` (mirrors the
   `approval-flows` template's pattern). Swap the in-memory store for
   `sqlitestore.Open` for a durable HTTP deployment — the engine works
   against either.

## Use this example when

You want to show that a Dockyard server isn't "either an analytics
server or an approval server" — it's both at once when the product
flow calls for it. Common shapes:

- Telemetry surface + a "fix it" approval (the example).
- Inventory dashboard + a "reorder N units" approval.
- Pull-request summary + a "merge" approval.

The shared App is the seam; keep the dispatcher's switch flat and the
patterns compose without coupling.

## Swap to a real backend

- `Snapshot.For` is the seam for `rollout_health` — replace its body
  with a call to your telemetry source (Prometheus, Datadog, an HTTP
  API). The typed contract is unchanged.
- `runApproval` is the seam for `propose_rollout_action` — wrap the
  body with a call into your real flag-management API (LaunchDarkly,
  Statsig, an internal feature service) immediately after the
  `Approved == true` branch.

## Pre-publish notes (D-139)

A scaffold built from this example via `dockyard new` would need
`go mod tidy` once after the scaffold; the example itself lives inside
the Dockyard repo and uses the root `go.mod`, so no extra step is
needed.

## Related

- [`examples/backend-tools-only`](../backend-tools-only) — pure-tools
  pattern (no UI).
- [`examples/prompts-demo`](../prompts-demo) — MCP Prompts via the
  Phase 28 prompts API (D-151).
- [`templates/analytics-widgets`](../../templates/analytics-widgets) —
  the read-side template the analytics half of this example
  generalises.
- [`templates/approval-flows`](../../templates/approval-flows) — the
  write-side template the approval half generalises.
