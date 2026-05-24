# Agent Skills

Dockyard ships a set of **Agent Skills** — directories under `skills/` in
the repository, each containing a `SKILL.md` file in the
[agentskills.io](https://agentskills.io) format. Each skill teaches an
AI coding agent one coherent Dockyard workflow.

An agent that supports the SKILL.md format (Claude Code, Cursor, OpenCode,
many others) discovers Dockyard's skills automatically when the
`skills/` directory is on its scan path. The validator at
`internal/skillcheck` enforces the format mechanically — a malformed
`SKILL.md` fails CI.

## The V1 skill set

| Skill                          | What it teaches                                                                               |
| ------------------------------ | --------------------------------------------------------------------------------------------- |
| `scaffold-a-server`            | `dockyard new` (blank + the two V1 templates); the `--dockyard-path` pre-publish replace      |
| `add-a-tool`                   | Define a typed Go contract pair, write the handler, register it, regenerate, validate         |
| `attach-a-ui-resource`         | The MCP App pattern: manifest, `//go:embed`, the bridge handshake, the deny-by-default CSP    |
| `define-contracts`             | Design A in depth: the Go struct as source of truth; `dockyard generate` + drift detection    |
| `run-the-dev-loop`             | `dockyard dev` — the embedded fsnotify orchestrator (Go restart + codegen + Vite supervision) |
| `validate`                     | `dockyard validate` + `dockyard test` — the full quality gate                                 |
| `package`                      | `dockyard build` + `dockyard run` + `dockyard install` — packaging and host wiring            |
| `test-with-the-inspector`      | `dockyard inspect` — fire tools by hand, switch fixtures, walk a task's lifecycle             |

Each skill lives at `skills/<slug>/SKILL.md` in the repo.

## Why agent skills

The bar (from
[AGENTS.md §19](https://github.com/hurtener/dockyard/blob/main/AGENTS.md)):

> A developer building with Dockyard via an agent should be productive
> from day one.

The skills cover the workflows that get a developer from "scaffold"
through "ship" without missing a step. They reference real
shipped surfaces — every command in every skill works against the
real `bin/dockyard` and the real templates today.

## Format

Per the
[agentskills.io specification](https://agentskills.io/specification):

```markdown
---
name: scaffold-a-server                              # required, lowercase
description: Scaffold a new Dockyard MCP server.     # required, ≤ 1024 chars
license: Apache-2.0                                  # optional
metadata:                                            # optional
  framework: dockyard
  surface: cli
---

# Body
…
```

The `name` field must match the parent directory name. The body is
plain Markdown — Dockyard's skills are typically 80–200 lines.

## The §19 hygiene rule

A PR that changes user-facing surface (a CLI verb, a manifest field,
a template, the generated-project shape, a public runtime API) **must
update the affected skill(s) and docs page(s) in the same PR.** The
rule is enforced by:

- The `internal/skillcheck` validator — fails CI when a `SKILL.md` is
  malformed.
- `scripts/drift-audit.sh`'s §19 hook — fails when a CLI verb has no
  referencing skill or docs page, or a shipped template has no docs
  walkthrough.

See [decision D-138](/reference/decisions) for the drift hook
rationale.

## See also

- The full [SKILL.md spec](https://agentskills.io/specification)
- [Getting started](/getting-started/) — the human path through the
  same surface
- [CLI reference](/cli/) — every verb in detail
