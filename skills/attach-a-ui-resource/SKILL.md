---
name: attach-a-ui-resource
description: Attach a Svelte MCP App (`ui://` resource) to a Dockyard MCP server, so a tool's structured output renders inline in the host's chat surface. Use when adding a UI to a blank-scaffold server, or adapting a template's App. Covers manifest wiring, the //go:embed bundle, the bridge handshake, host-theme propagation, and the deny-by-default CSP.
license: Apache-2.0
metadata:
  framework: dockyard
  surface: apps
  verbs: "generate validate build"
---

# Attach a Svelte UI resource to a Dockyard tool

A Dockyard tool's structured output (`tool.Result[Out].Structured`) can
render inline in the host's chat surface by attaching an **MCP App**: a
`ui://<server>/<app>` resource served with MIME
`text/html;profile=mcp-app`. The App is a Svelte page bundled by Vite into
a single HTML file, embedded into the Go binary at build time, and
served via the runtime's `runtime/apps` registration helper. The bridge
shell library (`web/bridge`) handles the `ui/` postMessage handshake;
inside the iframe you read the tool's structured payload as a typed value
and render it.

This skill walks through the parts. The two shipped V1 templates are the
canonical examples — start from one when you can.

## The five parts

1. **Declare the app in `dockyard.app.yaml`** — id, URI, entry file,
   display modes, CSP, visibility.
2. **Wire the tool to the app** — set `ui: <app-id>` on the tool entry
   (manifest) and `.UI(appName)` on the builder (`main.go`).
3. **Embed the bundle** — `//go:embed all:web/dist` in the server, and
   register the bundle via `apps.Register` in `main` (or `registerApp`).
4. **Author the Svelte App** — uses the in-repo `dockyard-bridge`
   helpers to receive the typed tool result and render.
5. **Build** — `dockyard build` runs Vite then `go build`, embedding the
   freshly built HTML.

## Prerequisites — the `web/` toolchain

The Dockyard **Go module** and the frontend packages **`dockyard-bridge`
and `dockyard-ui`** are all published, so a scaffolded UI project resolves
everything from npm + the Go proxy with no local checkout — `dockyard new
--template analytics-widgets` then `cd web && npm install` just works. The
hidden `--dockyard-path` flag remains a **build-from-source convenience**
(it points `web/` at a local Dockyard checkout via `file:` specs and adds
the `go.mod` `replace`); it is no longer required for a UI build.

## 1. Manifest declaration

In `dockyard.app.yaml`:

```yaml
apps:
  - id: widgets
    # The html-style `.../index.html` path matches the reference MCP Apps SDK
    # convention (D-178). The framework treats the ui:// URI as an OPAQUE string,
    # so the convention is documentation only — an existing project's
    # `ui://<server>/<app>` URI keeps working; only the convention moved.
    uri: ui://__PROJECT_NAME__/widgets/index.html
    entry: web/src/App.svelte
    # Inline only (D-126). The host renders the App in the chat surface.
    display_modes: [inline]
    csp:
      # Empty lists mean: the bundle is single-file, no external origins.
      # The deny-by-default CSP just works (RFC §7.4).
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

The `quality` block turns on the §20 four-state page rule —
loading/empty/error/permission states are mandatory and `dockyard
validate` will fail your build if any tool fixture is missing.

### Media in the App (`data:` / `blob:`, images, video)

The `csp` block models **domain allowlists**, not raw CSP directives:
`connect` → `connect-src`, `resource` → the static-asset directives
(`img-src` / `media-src` / `script-src` / `style-src` / `font-src`),
`frame` → `frame-src`, `base-uri` → `base-uri`. The **literal CSP string**
the iframe runs under — and in particular whether **`data:` and `blob:`**
URLs are permitted for images/video — is **built by the host**, not
declared by Dockyard. There is currently **no manifest knob to declare
`data:`/`blob:` media intent**.

Practical consequence for an image/video App: a single-file bundle inlines
small assets as `data:` URIs, but a host's deny-by-default CSP may block
`data:`/`blob:` media. **Design to degrade** — render a placeholder / empty
state when a `data:` thumbnail or `blob:` stream can't load, rather than
assuming it will. A first-class manifest declaration for this is tracked in
`docs/V2-BACKLOG.md` → "Apps `media-src` / `data:` / `blob:` declaration".

## 2. Wire the tool

In the manifest, set `ui: <app-id>` on each tool that drives the App:

```yaml
tools:
  - name: create_chart
    description: Render a chart inline in the host.
    input: internal/contracts.CreateChartInput
    output: internal/contracts.CreateChartOutput
    ui: widgets
    task_support: forbidden
```

In `main.go`'s `registerTools`, add `.UI(appName)` to the builder:

```go
return tool.New[contracts.CreateChartInput, contracts.CreateChartOutput]("create_chart").
    Describe("Render a chart inline in the host.").
    UI(appName).                       // <- attaches the tool to the App
    Handler(handlers.CreateChart).
    Register(srv)
```

`appName` is the same id you declared in the manifest (`widgets` above).
`.UI(appName)` is **sufficient** — at `Register` the builder resolves the
name to the App's `ui://` URI and emits `_meta.ui.resourceUri` on the tool
definition (RFC §7.1), the link a host needs to render the App. You never
hand-build `_meta`.

> **Ordering: register the App before the tools.** `.UI(appName)` resolves
> the name against the Apps registered on the server, so `registerApp(srv)`
> must run **before** `registerTools(srv)` in `main()` (the templates already
> do this). A `.UI("name")` that names no registered App is a **loud error**
> at `Register` — never a silent no-op — so a typo surfaces immediately.

**Visibility (optional).** Pass `tool.VisibilityApp` for a UI-only action
tool the model should not call directly (e.g. a "save edits" the App
invokes); omit it for the default (the host treats the tool as callable by
both the model and the App):

```go
tool.New[In, Out]("save_edits").
    UI(appName, tool.VisibilityApp).   // _meta.ui.visibility = ["app"]
    Handler(handlers.SaveEdits).
    Register(srv)
```

## 3. Embed the bundle + register

The Svelte App's built bundle lives under `web/dist/index.html` after
`dockyard build` runs Vite. Embed it and register at server startup:

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

The `all:` prefix is required — empty `_` and `.` files are skipped
otherwise (RFC §14, brief 06 §2.2).

### Dedicated origin (`App.Domain`) — host-supplied, remote-only, opt-in

Leave `App.Domain` **empty** unless you have a specific reason to set it — that
is the right default, and the host then serves your App from its default
per-conversation sandbox origin.

`_meta.ui.domain` is a **host-supplied verbatim value** (D-176). The MCP Apps
spec makes the format *host-dependent*: the host **mints** a dedicated
sandboxed-iframe origin and documents it (e.g. a `*.claudemcpcontent.com` or
`*.oaiusercontent.com` form); a server **copies that exact string** into
`App.Domain` and Dockyard emits it byte-for-byte. Dockyard does **not**
synthesise or derive it for you.

```go
return apps.Register(srv, apps.App{
    URI:   appURI,
    Name:  appName,
    HTML:  html,
    // Only for a verified REMOTE (HTTP) deployment, and only the exact origin
    // your host documents. Copy it verbatim; do not invent one.
    Domain: "a904794854a047f6.claudemcpcontent.com",
})
```

- **Remote-connector only.** A dedicated origin is honoured on a remote (HTTP)
  connector; a **local (stdio) connector ignores it**. If you set `Domain` on a
  stdio-only server, Dockyard logs a loud startup warning at `dockyard dev`/run
  naming the App — set `Domain` only for a verified remote deployment.
- **Deprecated.** `App.HostProfile` and `App.ServerURL` are deprecated and
  ignored (they previously drove a server-side derivation that the spec and the
  reference SDK showed was the wrong model). Don't set them.

### Wire-compat: the deprecated flat tool-UI `_meta` key (opt-in)

Dockyard emits the canonical **nested** `_meta.ui.resourceUri` on a tool, and by
default **never** the deprecated flat form. If you are targeting a host that
still reads the flat key, opt in server-wide:

```go
srv, _ := server.New(info, &server.Options{EmitLegacyToolUIMeta: true})
```

Every UI-bearing tool registered through the `tool.New(...).UI(...)` builder then
carries **both** keys (the flat value equals the nested `resourceUri`). Leave it
off (the default) for RFC-compliant nested-only output — the 2026-01-26 spec
marks the flat form deprecated. (D-177)

## 4. The Svelte App

The App receives the tool's `structuredContent` payload via the bridge.
A minimal dispatcher:

```svelte
<script lang="ts">
  import { hostContext, onToolResult } from 'dockyard-bridge';
  import Chart from './widgets/Chart.svelte';
  import Table from './widgets/Table.svelte';
  import MetricCardWidget from './widgets/MetricCardWidget.svelte';
  import type {
    CreateChartOutput,
    CreateTableOutput,
    CreateMetricCardOutput,
  } from './generated/contracts';

  let result: CreateChartOutput | CreateTableOutput | CreateMetricCardOutput | undefined;

  onToolResult((payload) => {
    result = payload;
  });
</script>

{#if !result}
  <p>Waiting for tool result…</p>
{:else if result.kind === 'chart'}
  <Chart data={result.data} type={result.type} theme={result.theme} />
{:else if result.kind === 'table'}
  <Table columns={result.columns} rows={result.rows} theme={result.theme} />
{:else if result.kind === 'metric_card'}
  <MetricCardWidget data={result} />
{/if}
```

The `Kind` discriminator on each output is the dispatcher's switch — the
`analytics-widgets` template uses this exact pattern.

### Rendering live task progress

A tool backed by a long-running task (`TaskHandle`) reports progress
server-side with `h.Progress(ctx, fraction, message)`. To render a live
"62%" inside the App's card, subscribe to `onTaskProgress` — the host
forwards each progress point as a `ui/notifications/task-progress`
notification (RFC §8.4):

```svelte
<script lang="ts">
  import { onTaskProgress } from 'dockyard-bridge';
  let percent: number | undefined;
  let note = '';
  onTaskProgress((p) => {
    if (p.fraction !== undefined) percent = Math.round(p.fraction * 100);
    if (p.message) note = p.message;
  });
</script>

{#if percent !== undefined}
  <progress value={percent} max="100"></progress>
  <span>{percent}% — {note}</span>
{/if}
```

The channel degrades cleanly: a host that does not forward task progress
simply never fires the subscriber, so subscribe unconditionally and render
whatever arrives. It is demoable through `dockyard inspect` — the inspector
forwards the attached server's task progress to the App preview.

> **Tasks×Apps is Dockyard-host-only.** `onTaskProgress` and the
> elicitation-response flow are **Dockyard extensions** outside the MCP Apps
> schema (`task-progress` / `elicitation-response`). They work **only against a
> Dockyard-aware host** — the inspector, or Harbor as the MCP client. A stock
> host (e.g. Claude Desktop) ignores them: progress never arrives and an
> elicitation reply is dropped. Build the App so its core value does not depend
> on them; treat live progress and inline elicitation as enhancements that light
> up on a Dockyard host.

### Host theme propagation

Read the host's theme from `hostContext.styles.variables`. The bridge
auto-propagates it on the handshake; no per-call wiring needed. If your
tool's contract has an explicit `theme` field (analytics-widgets does),
let the handler resolve `"auto"` against `hostContext` and pass the
resolved value forward.

### The four UI states

Every page renders through the `PageState` four-state pattern
(loading / empty / error / permission / ready — AGENTS.md §20). Make sure
each fixture exercises one state, and the host theme is honoured in each.

## 5. Build and verify

```bash
dockyard build           # Vite then go build; embeds web/dist/index.html
dockyard validate        # checks the app↔tool wiring, CSP, MIME, four-state
dockyard inspect --url <server>   # open the app preview, fire the tool
```

The inspector's App preview reads the live `ui://` resource via a
short-lived, operator-initiated MCP client session (D-103, D-144).
You should see your App render the tool result with realistic
synthetic data from the fixture switcher (D-130).

> **Don't "tidy" the generated Vite config.** It emits `format: 'iife'`
> and applies a small `stripModuleType` plugin on purpose: the App renders
> in a **sandboxed iframe without `allow-same-origin`**, where browsers
> refuse to execute `<script type="module">`. An IIFE bundle with the
> `type="module"` attribute stripped is the one shape that runs in that
> sandbox. Switching the config back to an ES-module build (or dropping the
> plugin) silently breaks the App in the host with **no build error** — it
> just won't execute. Leave both in place.

## Common pitfalls

- **App and tool out of sync.** A tool with `ui: widgets` but no `apps[]`
  entry named `widgets` is a `dockyard validate` blocker — fix the
  manifest.
- **Missing `//go:embed all:web/dist`.** Without it, the server
  cannot find `web/dist/index.html` at runtime — `apps.Register` returns
  an error. Run `dockyard build` (or `npm run build` in `web/`) before
  `go build` so the bundle exists.
- **CSP too tight or too loose.** Empty `connect: []` / `resource: []`
  for a single-file bundle is the deny-by-default sweet spot. Add an
  origin only when your App genuinely needs to fetch from it.
- **Forgetting `.UI(appName)` on the tool builder.** The manifest's
  `ui: <id>` is half the wiring; the runtime needs the builder call too
  so the `_meta.ui` block on the tool result points at the right
  resource.
- **Blank/white App in the host (renders fine in the inspector).** Use a
  current `dockyard-bridge` (≥ 1.6.1): earlier builds spoke a non-spec
  `ui/initialize` handshake a strict host rejected, and never reported the
  App's size, so the host collapsed the iframe to ~0px (D-179, D-180,
  D-181). Then check the iframe console for CSP `Refused to …` errors and
  declare the missing origin in the manifest `csp`.

## What to do next

- Define more contracts that drive the App ⇒ `define-contracts` skill.
- Live-edit + see HMR-style reload in the inspector ⇒
  `run-the-dev-loop` skill.
- Add task-augmented tools (e.g. approvals) ⇒ start from the
  `approval-flows` template; the bridge ships the
  `ui/elicitation-response` notification (D-134).
- Ship the binary ⇒ `package` skill.
