# UI resources (MCP Apps)

An MCP App is a `ui://<server>/<app>` resource served with MIME
`text/html;profile=mcp-app` — a Svelte page bundled by Vite into a
single HTML file, embedded into the Go binary at build time, and
registered via `runtime/apps`. The Dockyard bridge shell (`web/bridge`)
handles the `ui/` postMessage handshake so your App reads the tool's
structured payload as a typed value and renders.

## The five parts

1. Declare the app in `dockyard.app.yaml`.
2. Wire each tool to the app with `ui: <id>` (manifest) and
   `.UI(appName)` (Go builder).
3. Embed the bundle with `//go:embed all:web/dist`.
4. Author the Svelte App.
5. Build with `dockyard build` — Vite first, then `go build`.

## Manifest declaration

```yaml
apps:
  - id: widgets
    uri: ui://__PROJECT_NAME__/widgets
    entry: web/src/App.svelte
    display_modes: [inline]
    csp:
      connect: []
      resource: []
    visibility: [model, app]

quality:
  require_loading_state: true
  require_empty_state: true
  require_error_state: true
  require_permission_state: true
  require_fixtures: true
```

`display_modes: [inline]` is the V1 default (decision
[D-126](/reference/decisions)). The empty CSP lists pair with a
single-file bundle (no external origins) and give you the
deny-by-default posture by construction (RFC §7.4).

The `quality:` block turns on the [four-state page rule](/reference/design-conventions)
(AGENTS.md §20): every page renders through loading / empty / error /
permission / ready, and `dockyard validate` fails the build if a
fixture is missing.

## Wire the tool

```yaml
tools:
  - name: create_chart
    description: Render a chart inline in the host.
    input: internal/contracts.CreateChartInput
    output: internal/contracts.CreateChartOutput
    ui: widgets
    task_support: forbidden
```

```go
return tool.New[contracts.CreateChartInput, contracts.CreateChartOutput]("create_chart").
    Describe("Render a chart inline in the host.").
    UI(appName).               // attach to the widgets App
    Handler(handlers.CreateChart).
    Register(srv)
```

## Embed + register

```go
//go:embed all:web/dist
var uiBundle embed.FS

const (
    appURI  = "ui://__PROJECT_NAME__/widgets"
    appName = "widgets"
)

func registerApp(srv *server.Server) error {
    html, err := fs.ReadFile(uiBundle, "web/dist/index.html")
    if err != nil {
        return err
    }
    return apps.Register(srv, apps.App{
        URI:   appURI,
        Name:  appName,
        Title: "__PROJECT_TITLE__ — widgets",
        HTML:  html,
    })
}
```

The `all:` prefix preserves hashed asset names and includes `_` and `.`
files. The single-file bundle pattern is preferred (one HTML file with
inlined assets) — see the `analytics-widgets` template's Vite config for
the canonical setup.

## Author the Svelte App

```svelte
<script lang="ts">
  import { hostContext, onToolResult } from '@dockyard/bridge';
  import Chart from './widgets/Chart.svelte';
  import Table from './widgets/Table.svelte';

  let result: any;
  onToolResult((p) => { result = p; });
</script>

{#if !result}
  <p>Waiting for tool result…</p>
{:else if result.kind === 'chart'}
  <Chart {...result} />
{:else if result.kind === 'table'}
  <Table {...result} />
{/if}
```

The host theme arrives on `hostContext.styles.variables`; the bridge
propagates it automatically.

## Build + verify

```bash
dockyard build
dockyard validate
dockyard inspect --url <server> --dir .
```

The inspector renders your App in a sandboxed iframe (the same CSP
your manifest declares). The App preview fetches the `ui://` resource
via `resources/read` in a short-lived, operator-initiated MCP client
session (decisions [D-103](/reference/decisions),
[D-144](/reference/decisions)); the inspector itself never executes
server side-effects on its own — every mutating call comes from an
explicit UI action.

## See also

- [`attach-a-ui-resource` agent skill](/agent-skills/)
- [`analytics-widgets` walkthrough](/getting-started/analytics-widgets)
- [`approval-flows` walkthrough](/getting-started/approval-flows)
- [Design conventions](/reference/design-conventions)
