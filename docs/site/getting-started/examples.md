# Worked examples

The two shipped templates (`analytics-widgets`, `approval-flows`) cover
the common "scaffold a new server" path. Three worked **examples**
complement them — reference implementations for patterns developers
reach for once they're past the scaffold step.

Examples live under [`examples/`](https://github.com/hurtener/dockyard/tree/main/examples)
in the repo. Unlike templates, they are not scaffolded by `dockyard new`
— they are reference projects a developer reads + copies (decision
[D-150](/reference/decisions)). Each example builds, validates, and
runs end-to-end against the current runtime.

## The three examples

### [`backend-tools-only`](https://github.com/hurtener/dockyard/tree/main/examples/backend-tools-only)

**Pure-tools MCP server — no MCP App, no UI.** The common case for a
developer who just wants to expose tools to an agent host. Demonstrates
that Dockyard is the right framework for backend-only servers, not only
UI-bearing ones.

Three typed tools over an in-memory bookmarks catalog
(`list_bookmarks`, `add_bookmark`, `search_bookmarks`). The manifest
declares only tools — no `apps[]` block. Swap the in-process catalog
for a real backing store and the typed contracts are unchanged.

### [`combined-patterns`](https://github.com/hurtener/dockyard/tree/main/examples/combined-patterns)

**Analytics widget + approval flow composed on one MCP App.** Shows
that the two shipped templates aren't isolated — they combine into a
real product flow: *insight → action*.

The domain is a feature-flag rollout reviewer: a `rollout_health` tool
returns a metric card (analytics-widgets pattern), and a
`propose_rollout_action` tool pauses for human approval of the next
ramp (approval-flows pattern). Both renderers dispatch on
`structuredContent.kind` from the same App, exactly like the templates.

### [`prompts-demo`](https://github.com/hurtener/dockyard/tree/main/examples/prompts-demo)

**Three MCP Prompts via Dockyard's prompts API**
(`runtime/server.AddPrompt`; decision [D-151](/reference/decisions)).
Demonstrates the tool / prompt distinction MCP makes:

- Tools — things the model PUSHES (typed input → action / answer).
- Prompts — templates the host PULLS (named slot-in templates that
  seed a chat or a sub-agent).

Three prompts (`summarize_for_review`, `code_review`, `explain_error`)
plus one tool (`summarize_text`) so the manifest stays valid and the
tool / prompt shapes sit side-by-side.

Dockyard's contract-first pattern does not extend naturally to prompts
— MCP prompt arguments are a flat string-keyed map, not a structured
object (decision [D-152](/reference/decisions)). `AddPrompt` is a
focused, registration-only surface; for an argument shape that needs
structured validation, register a **tool** instead.

## Running an example

Each example is a buildable Go program inside the Dockyard repo. From
the repo root:

```bash
cd examples/<slug>
dockyard generate                  # generate JSON Schema + TS for the tool contracts
dockyard validate                  # validate the manifest + contracts wiring
go test ./internal/handlers        # run the example's contract tests
go run ./cmd/server                # serve over stdio (the default transport)

# Or, serve over streamable-HTTP and attach the inspector:
DOCKYARD_TRANSPORT=http go run ./cmd/server &
dockyard inspect --url http://127.0.0.1:8080
```

See each example's `README.md` for the full lifecycle + the
swap-to-a-real-backend notes.

## Examples vs templates

| | Templates | Examples |
|---|---|---|
| **Where they live** | `templates/<slug>/` | `examples/<slug>/` |
| **Entry point** | `dockyard new --template <slug>` | `git clone` + read |
| **Substitution** | Token-replaced at scaffold time | None — fixed in-tree |
| **Embedded in binary** | Yes (`builtin.go` + `//go:embed`) | No (D-150) |
| **Reach for when** | Starting a new project | Looking for a reference pattern |

## Related

- [analytics-widgets walkthrough](analytics-widgets) — the read-side
  template walkthrough.
- [approval-flows walkthrough](approval-flows) — the write-side
  template walkthrough.
- [Decisions log: D-150, D-151, D-152](/reference/decisions) — the
  examples + prompts decisions.
