# Inspector

The inspector is Dockyard's local **test + debug** surface
([RFC §12](/reference/rfc)). It is:

- **Dev-mode-gated, localhost-only, operator-initiated only.** Never a
  production client; never reachable off-localhost (P4 in §1). "Operator-
  initiated only" — re-cast from the older "read-only" framing in
  [D-144](/reference/decisions) — means every client-shaped operation is
  driven by an explicit operator UI action (a button click), runs in a
  short-lived per-request MCP client session, and has a documented
  decision entry (D-099 / D-103 / D-131 / D-134) explaining why it stays
  within P4.
- **A pure Logbook consumer.** It reads no runtime internals — every
  signal it shows is an emitted Logbook event (P2).
- **Wired to your project.** Verdicts re-run `dockyard validate`; the
  Fixtures switcher derives from the project's generated tool
  contracts; the App preview reads the running server's `ui://`
  resources via short-lived MCP client sessions, scoped to operator-
  initiated UI actions (decisions
  [D-103](/reference/decisions), [D-130](/reference/decisions),
  [D-144](/reference/decisions)).

## Attach

In one terminal, run the server on HTTP (the inspector relays via
HTTP):

```bash
DOCKYARD_TRANSPORT=http dockyard run
```

In a second terminal, attach the inspector:

```bash
dockyard inspect --url http://127.0.0.1:8080 --dir path/to/project
```

| Flag        | What                                                                  |
| ----------- | --------------------------------------------------------------------- |
| `--url`     | the running MCP server's base URL; required to attach                 |
| `--dir`     | the project directory; powers Verdicts + Fixtures                     |
| `--port`    | the inspector's loopback port (default: OS-assigned)                  |
| `--no-open` | don't open a browser — CI / headless use                              |

A non-loopback `--port` host is refused before the listener opens
(the CVE-2025-49596 lesson).

## Rail tabs

| Tab        | What it shows / does                                                       |
| ---------- | -------------------------------------------------------------------------- |
| Tools      | All registered tools; fire one (the Operator-Invoke surface, [D-131](/reference/decisions)) |
| Apps       | Each `ui://` resource rendered in a sandboxed iframe                       |
| Tasks      | Active + recent tasks rendered as a lifecycle Timeline                     |
| Events     | The live Logbook stream                                                   |
| Analytics  | Per-tool latency + counts                                                  |
| Fixtures   | Pick a UI state for the App preview                                        |
| Verdicts   | Re-runs `dockyard validate`                                                |
| RPC        | Raw JSON-RPC log                                                           |

![tools-invoke](/screenshots/phase-24-finish/tools-invoke.png)

![events](/screenshots/phase-24-finish/events.png)

## The Fixtures switcher

Each tool ships six default fixtures
([D-130](/reference/decisions)):

| Fixture       | Drives                                                |
| ------------- | ----------------------------------------------------- |
| `happy`       | ready state with realistic synthetic payload          |
| `empty`       | empty arrays / nil branches                            |
| `error`       | handler-returned error path                            |
| `permission`  | permission-denied stub                                 |
| `slow`        | deliberately delayed response                          |
| `large`       | stress-sized payload                                   |

On-disk project fixtures (`<dir>/fixtures/<tool>/<kind>.json`) override
the schema-derived synthetic ones.

![fixtures](/screenshots/phase-24-finish/fixtures-with-logo.png)

## Capability emulation

Flip Apps, Tasks, the `logging` capability on/off to verify your
server degrades gracefully on a host that hasn't negotiated that
capability (RFC §7.5, the capability-driven rule from
[AGENTS.md §6](https://github.com/hurtener/dockyard/blob/main/AGENTS.md)).

## Task lifecycle

For task-augmented tools, the Tasks tab renders the lifecycle as a
Timeline (`created → working → input_required → working →
completed`):

![tasks panel](/screenshots/phase-25/tasks-panel-live.png)

## Why this is different

mcp-use's inspector is interactive but **not a test harness** (brief
[04 §2.5](https://github.com/hurtener/dockyard/blob/main/docs/research/04-mcp-use-dx-teardown.md)):
no fixture system, no scripted state-switch, no contract drift
catcher, no host-compatibility matrix. Dockyard's inspector is wired
to the project — the same checks `dockyard validate` runs, the same
fixtures `dockyard test` exercises, the same Logbook stream the
runtime emits. The inspector is the interactive face of the same
quality gates.

## See also

- [`test-with-the-inspector` agent skill](/agent-skills/)
- [Dev loop guide](dev-loop)
- [Decisions: D-103, D-130, D-131](/reference/decisions)
