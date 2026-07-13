---
name: test-with-the-inspector
description: Drive and debug a Dockyard MCP server through Dockyard's local inspector (`dockyard inspect`). Use to invoke tools by hand, switch fixtures across UI states (happy/empty/error/permission/slow/large), watch the live Logbook stream, render Apps in a sandboxed iframe, and walk a task's lifecycle in the Tasks panel. Dev-mode-gated, localhost-only, operator-initiated only (D-144).
license: Apache-2.0
metadata:
  framework: dockyard
  surface: inspector
  verbs: "inspect"
---

# Test a Dockyard server with the local inspector

The inspector is Dockyard's local **test + debug** surface. It is:

- **Dev-mode-gated, localhost-only, operator-initiated only.** Never a
  production client; never reachable off-localhost (RFC §12, P4 in §1).
  "Operator-initiated only" — re-cast from the older "read-only" framing
  in D-144 — means every client-shaped operation is driven by an
  explicit UI action (a button click), runs in a short-lived per-request
  MCP client session, and has a documented decision entry (D-099, D-103,
  D-131, D-134) explaining why it stays within P4.
- **A pure Logbook consumer.** It reads no runtime internals; every
  signal it shows is an emitted Logbook event (P2).
- **Wired to your project.** Verdicts re-run `dockyard validate`; the
  Fixtures switcher derives from the project's generated tool contracts
  (D-130); the App preview reads the running server's `ui://`
  resources via short-lived operator-initiated sessions (D-103, D-144).

## Attach to a running server

The fastest path is `dockyard dev` — it **auto-attaches** the
inspector as a third supervised child alongside the Go server and
Vite, and prints the inspector URL to stdout once it is reachable.
You skip the second terminal entirely:

```bash
dockyard dev
# ...
# INFO inspector ready at http://127.0.0.1:54321
```

Pass `--no-inspector` (CI / headless) to skip the supervised
inspector child.

For a server that is not under `dockyard dev` (e.g. a deployed dev
build, a remote loopback server you are debugging), use the
standalone path. In one terminal, run the server on HTTP (the
inspector relays via HTTP):

```bash
DOCKYARD_TRANSPORT=http dockyard run
```

In a second terminal, attach the inspector:

```bash
dockyard inspect --url http://127.0.0.1:8080
```

Or attach to a project directory and let `inspect` print the URL it
chose:

```bash
dockyard inspect --url http://127.0.0.1:8080 --dir path/to/project
```

`inspect`'s flags:

| Flag        | What it does                                                          |
| ----------- | --------------------------------------------------------------------- |
| `--url`     | the running MCP server's base URL; required to attach                 |
| `--dir`     | the Dockyard project directory (default: cwd); powers Verdicts + Fixtures |
| `--port`    | the inspector's own loopback port (default: OS-assigned)              |
| `--no-open` | do not open a browser — for CI and headless use                       |

A non-loopback `--port` host is refused before the listener opens —
mechanical enforcement of RFC §12 (the CVE-2025-49596 lesson).

The inspector deliberately does not implement OAuth authorization-code/PKCE,
accept a bearer-token flag, forward credentials, or store tokens. Attaching to
an OAuth-protected endpoint therefore surfaces its challenge and stops; it never
downgrades around authorization. Use Harbor or a purpose-built test client for
the protected path, and an unauthenticated loopback-only configuration for local
inspector work. Configuration details are in
`docs/site/guides/oauth-protected-resource.md`.

## The rail tabs

| Tab        | What it shows / does                                                       |
| ---------- | -------------------------------------------------------------------------- |
| Tools      | All registered tools; click one to fire it (the Operator-Invoke surface — D-131) |
| Prompts    | All registered MCP prompts; pick one, fill its arguments, render `prompts/get` messages (D-163) |
| Apps       | Each `ui://` resource rendered in a sandboxed iframe (D-103)               |
| Tasks      | The active and recent tasks, rendered as a lifecycle Timeline               |
| Events     | The live Logbook stream — every tool/resource/app/task event              |
| Analytics  | Per-tool latency + counts derived from Logbook                            |
| Fixtures   | The fixture switcher — pick a UI state for the App preview                 |
| Verdicts   | Re-runs `dockyard validate`; renders blockers + warnings                  |
| RPC        | The raw JSON-RPC log between the inspector and the attached server         |

## The Prompts panel

MCP separates two model-facing primitives:

- **Tools** — things the model PUSHES (a typed input becomes a typed
  output, the host validates, the runtime emits a Logbook event).
- **Prompts** — templates the host PULLS via `prompts/get` (named
  curated message sets a chat host surfaces as `/slash` commands or
  quick-action buttons; the user picks one, the host fills its
  arguments, the model is seeded).

The Prompts panel lists every prompt the attached server registered
via `runtime/server.AddPrompt`. Pick one, fill its string arguments
(MCP prompt arguments are flat strings — see D-152; no JSON Schema
form), press **Invoke prompts/get**. The inspector opens a
short-lived MCP client session, calls `prompts/get`, closes the
session, and renders the resulting message list — one card per
message, role chip + the text body. A server-side error renders in
the result region (the 200-with-error pattern); a transport-level
failure renders in the panel's `ErrorState` with a working retry.

The short-lived base MCP connection attempts modern `2026-07-28`
`server/discover` first and falls back to legacy `2025-11-25` `initialize` only
for a compatible peer.

Use the Prompts panel to:

- Verify a `server.AddPrompt` registration is reachable end-to-end.
- Inspect what a host actually sees when it pulls one of your prompts
  — exact roles, exact text, exact substitutions.
- Compare two argument variants quickly (re-fill, re-invoke; the
  result region updates in place).

Try it against `examples/prompts-demo` — three prompts (`summarize_for_review`,
`code_review`, `explain_error`) exercise the panel end-to-end.

## The Operator-Invoke flow

For any tool, the Tools tab has:

- A schema-derived form with one field per input property.
- A fixture switcher (the same six fixtures the App preview uses).
- An "Invoke" button — clicking it sends a real `tools/call` to the
  attached server and renders the structured result in the App preview
  if the tool has a `_meta.ui` block, or as raw JSON otherwise.

Use Invoke to:

- Confirm a new tool you just added (`add-a-tool` skill) actually
  registers and returns a sensible payload.
- Drive a tool with each of its fixtures, exercising every UI state.
- Reproduce a host-side bug by firing the same call the host would
  make.

## The Fixtures switcher

Each tool ships six default fixtures (D-130):

| Fixture       | What it drives                                              |
| ------------- | ----------------------------------------------------------- |
| `happy`       | the "ready" state — realistic synthetic payload             |
| `empty`       | the "empty" state — empty arrays / nil branches             |
| `error`       | the "error" state — a handler-returned error path           |
| `permission`  | the "permission" state — a permission-denied stub           |
| `slow`        | the "slow" state — a deliberately delayed response          |
| `large`       | the "large" state — a stress-sized payload                  |

On-disk project fixtures (`<dir>/fixtures/<tool>/<kind>.json`) are
preferred over the schema-derived synthetic fixtures (D-130). The
`analytics-widgets` template ships all six per tool — open one as a
reference when authoring your own.

## App preview

Each `ui://` resource renders in a sandboxed iframe with the
deny-by-default CSP the manifest declares. The bridge handshake
completes on render; once the App is up, fire its tool from the Tools
tab and the App receives the structured result through the bridge
exactly as a real host would deliver it.

The bridge handshake is Apps `2026-01-26` `ui/initialize`. Do not confuse it
with base MCP `server/discover`; they have separate version spaces and run on
different boundaries.

## Capability emulation

The inspector can flip host capabilities on/off — Apps, Tasks, the
`logging` capability — to verify your server degrades gracefully on a
host that does not negotiate that capability (RFC §7.5, the
capability-driven rule from AGENTS.md §6). If your App is mandatory in
the workflow, emulating "no Apps" should produce a working text-only
response from your tool, not a crash.

## Task lifecycle

For task-augmented tools (the `approval-flows` template's two tools
are the canonical examples), the Tasks tab renders the lifecycle as a
Timeline:

```text
created → working → input_required → working → completed
```

You can:

- Watch task `input_required` state from `tasks/get`, respond through
  `tasks/update`, and see polling resume.
- Cancel a running task — the handler's `ctx.Done()` /
  `TaskHandle.Cancelled()` should fire, and the task moves to
  `cancelled` with the App rendering its "withdrawn" empty state.

Core MRTR is a separate flow. For an input-required `tools/call`,
`prompts/get`, or `resources/read`, the inspector collects the operator's
response and retries the original method with a new JSON-RPC ID,
`inputResponses`, and the opaque `requestState` returned by the server. It does
not create or update a task. Conversely, task input has no core `requestState`
and never retries the original method.

The inspector uses `dockyard/tasks/supplyInput` only for a peer that negotiated
the legacy protocol. Modern task input always uses `tasks/update`.

## Why this matters

mcp-use's inspector is interactive but **not a test harness** (brief 04
§2.5): no fixture system, no scripted state-switch, no contract drift
catcher, no host-compatibility matrix. Dockyard's inspector is wired
to the project — the Verdicts tab re-runs `dockyard validate`, the
Fixtures switcher derives from the generated contracts, capability
emulation is one toggle, the App preview honours the CSP. You can
encode "this is correct" and have CI enforce it; the inspector is the
interactive face of the same checks.

## Common pitfalls

- **"Refusing non-loopback bind"** — the inspector refused a `--port`
  whose host is not loopback. Use 127.0.0.1 or omit `--port`.
- **App preview is blank.** The bridge handshake didn't complete —
  check the JSON-RPC log (RPC tab) for the first `ui/` notification;
  a missing or malformed `_meta.ui` block on the tool result is the
  usual cause.
- **No Tasks tab activity.** The attached server is not task-augmented
  (the manifest's tools all declare `task_support: forbidden` or
  `optional`). Set `task_support: required` for the tools you want to
  observe through the lifecycle, or use the `approval-flows` template.
- **The Operator-Invoke button is greyed out.** The attached server is
  not reachable; check the URL and that the server is running.

## What to do next

- Build a fresh tool ⇒ `add-a-tool` skill.
- Attach a UI to a tool ⇒ `attach-a-ui-resource` skill.
- Iterate live with the inspector open ⇒ `run-the-dev-loop` skill.
