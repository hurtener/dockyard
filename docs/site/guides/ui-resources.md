# UI resources (MCP Apps)

An MCP App is a `ui://<server>/<app>/index.html` resource served with MIME
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
    uri: ui://__PROJECT_NAME__/widgets/index.html
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
    appURI  = "ui://__PROJECT_NAME__/widgets/index.html"
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

::: tip The `ui://` URI is an opaque string
The html-style `.../index.html` path matches the reference MCP Apps SDK
convention ([D-178](/reference/decisions)). Dockyard treats the URI as an
opaque identifier, so the convention is documentation only — an existing
project's `ui://<server>/<app>` URI keeps working; only the convention moved.
:::

## Dedicated origin (`domain`)

Leave `App.Domain` **empty** unless you have a specific reason to set it. The
host then serves your App from its default per-conversation sandbox origin.

`_meta.ui.domain` is a **host-supplied, verbatim** value
([D-176](/reference/decisions)). The MCP Apps spec makes its format
*host-dependent*: the host **mints** a dedicated sandboxed-iframe origin and
documents it (e.g. a `*.claudemcpcontent.com` or `*.oaiusercontent.com` form);
a server copies that exact string into `App.Domain` and Dockyard emits it
byte-for-byte. Dockyard never synthesises or derives it.

A dedicated origin is honoured only on a **remote (HTTP) connector** — a local
(stdio) connector ignores it. If you set `Domain` on a stdio-only server,
Dockyard logs a loud startup warning naming the App; set it only for a verified
remote deployment. (The former `App.HostProfile` / `App.ServerURL` fields are
deprecated and ignored.)

## Wire-compat: the deprecated flat tool-UI `_meta` key

Dockyard emits the canonical **nested** `_meta.ui.resourceUri` on a tool and, by
default, never the deprecated flat form. For a host that still reads the flat
key, opt in server-wide:

```go
srv, _ := server.New(info, &server.Options{EmitLegacyToolUIMeta: true})
```

Every UI-bearing tool registered through the `tool.New(...).UI(...)` builder then
carries **both** keys (the flat value equals the nested `resourceUri`). Leave it
off (the default) for RFC-compliant nested-only output — the 2026-01-26 spec
marks the flat form deprecated. ([D-177](/reference/decisions))

## Author the Svelte App

```svelte
<script lang="ts">
  import { hostContext, onToolResult } from 'dockyard-bridge';
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

## Troubleshooting: a blank App in the host

If your App renders fine in the inspector but shows as a blank/white area
in a host like Claude Desktop, work through these in order:

- **Use a current `dockyard-bridge` (≥ 1.6.1).** Earlier bridge builds
  spoke a non-spec handshake that a strict host rejected (or deadlocked
  against), and never reported the App's content size — so the host sized
  the iframe to ~0px and the App looked blank with no error. The current
  bridge speaks the host's `ui/` dialect, signals readiness itself, and
  reports its size automatically (decisions
  [D-179](/reference/decisions), [D-180](/reference/decisions),
  [D-181](/reference/decisions)).
- **Check the iframe console for CSP errors.** The App runs under a
  deny-by-default sandbox. A `Refused to connect/load …` error means a
  domain your App reaches at runtime isn't declared — add it to the
  manifest `csp` (`connect` for `fetch`/WebSocket, `resource` for
  scripts/styles/fonts/images).
- **Keep the tool result small.** Very large results can fail to render;
  return only what the App needs as structured output.

## Tasks×Apps is Dockyard-host-only

Live task progress (`onTaskProgress`) and the inline elicitation-response flow
are **Dockyard extensions** — the `ui/notifications/task-progress` and
`ui/notifications/elicitation-response` messages are not part of the MCP Apps
schema. They work **only against a Dockyard-aware host**: the local inspector,
or Harbor acting as the MCP client. A stock host (for example Claude Desktop)
ignores them — progress never arrives and an elicitation reply is dropped
(decision [D-183](/reference/decisions)). Design an App so its core value works
without them, and treat progress and inline elicitation as enhancements that
light up on a Dockyard host.

## See also

- [`attach-a-ui-resource` agent skill](/agent-skills/)
- [`analytics-widgets` walkthrough](/getting-started/analytics-widgets)
- [`approval-flows` walkthrough](/getting-started/approval-flows)
- [Design conventions](/reference/design-conventions)
