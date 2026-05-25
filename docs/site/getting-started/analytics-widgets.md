# `analytics-widgets` — read-side walkthrough

The `analytics-widgets` template is the canonical read-side Dockyard
example ([D-124](/reference/decisions)). Three contract-first widget
tools rendered inline by one Svelte App.

## Scaffold

```bash
dockyard new my-widgets \
  --template analytics-widgets \
  --dockyard-path /path/to/dockyard   # pre-publish only
cd my-widgets
```

The scaffold produces:

```text
my-widgets/
├── README.md
├── dockyard.app.yaml          # manifest — three tools + one App
├── go.mod
├── main.go                    # registers app + tools; stdio | http serve
├── internal/
│   ├── contracts/             # CreateChart{Input,Output}, CreateTable…, MetricCard…
│   └── handlers/              # CreateChart, CreateTable, CreateMetricCard
├── fixtures/                  # six fixtures per tool — happy/empty/error/permission/slow/large
└── web/
    ├── package.json
    ├── vite.config.ts
    └── src/
        ├── App.svelte         # the dispatcher (by Kind discriminator)
        ├── theme.ts
        └── widgets/
            ├── Chart.svelte
            ├── ChartFrame.svelte
            ├── Table.svelte
            └── MetricCardWidget.svelte
```

## The three tools

| Tool                  | Renders                                                |
| --------------------- | ------------------------------------------------------ |
| `create_chart`        | Apache-ECharts chart inline (bar/line/area/pie/scatter/radar) |
| `create_table`        | sortable, paged data table                              |
| `create_metric_card`  | KPI card with optional sparkline + breakdown            |

Each tool is contract-first: the typed input/output structs in
`internal/contracts/contracts.go` are the source of truth; the JSON
Schema the host sees is generated.

The output of each tool carries a `Kind` discriminator (`"chart"`,
`"table"`, `"metric_card"`) so the App's single dispatcher routes
`structuredContent` to the right renderer with no shape-sniffing.

## Run + inspect

```bash
# One-time after a pre-publish scaffold:
go mod tidy

# A template scaffold ships the Go contracts but not the generated
# JSON Schema + TS — produce them once:
dockyard generate

# Build the project once so web/dist exists
dockyard build

# Run on HTTP so the inspector can attach
DOCKYARD_TRANSPORT=http dockyard run

# In another terminal:
dockyard inspect --url http://127.0.0.1:8080 --dir .
```

The inspector renders the App in a sandboxed iframe. The Fixtures
switcher cycles through the six per-tool fixtures so you can see each
UI state without writing a real call:

![chart](/screenshots/analytics-widgets/chart.png)

![table](/screenshots/analytics-widgets/table.png)

![metric-card](/screenshots/analytics-widgets/metric-card.png)

Fire a tool from the Tools tab and watch the App receive the structured
result through the bridge:

![operator invoke](/screenshots/phase-24-finish/tools-invoke.png)

The Events tab shows the live `obs/v1` stream — every tool call lands
as a `tool.completed` event:

![events](/screenshots/phase-24-finish/events.png)

The Verdicts tab re-runs `dockyard validate` — green when the project
is clean:

![verdicts](/screenshots/phase-24-finish/verdicts.png)

The Analytics tab plots per-tool latency derived from `obs/v1`:

![analytics](/screenshots/phase-24-finish/analytics.png)

## How the App dispatches

`web/src/App.svelte` listens for the tool result and picks a renderer
by `Kind`. Sketch:

```svelte
<script lang="ts">
  import { onToolResult } from '@dockyard/bridge';
  import Chart from './widgets/Chart.svelte';
  import Table from './widgets/Table.svelte';
  import MetricCardWidget from './widgets/MetricCardWidget.svelte';

  let result: any;
  onToolResult((p) => { result = p; });
</script>

{#if result?.kind === 'chart'}
  <Chart {...result} />
{:else if result?.kind === 'table'}
  <Table {...result} />
{:else if result?.kind === 'metric_card'}
  <MetricCardWidget {...result} />
{/if}
```

The host theme is propagated automatically via
`hostContext.styles.variables`; each contract has an explicit `theme`
field so a tool call can override the host default.

## Adapt it

- Add a fourth widget — define the contracts in
  `internal/contracts/contracts.go`, write the handler in
  `internal/handlers/`, register it in `main.go`, add an entry to
  `dockyard.app.yaml`. Run `dockyard generate` then `dockyard validate`
  (see the [Contracts guide](/guides/contracts) and the
  [`add-a-tool` skill](/agent-skills/)).
- Plug a real data source — replace the synthetic body of each handler
  with a call to your service or database. The typed contract is the
  integration surface; the rest of the App is unchanged.
- Tune the theme — `web/src/theme.ts` overrides the design tokens; the
  bridge merges the host theme over your overrides.

## What next

- The other template — [approval-flows walkthrough](approval-flows) —
  the write-side example.
- [UI resources guide](/guides/ui-resources) — the App pattern in detail.
- [Inspector guide](/guides/inspector) — every rail tab + flag.
