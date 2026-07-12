---
layout: home

hero:
  name: "Dockyard"
  text: "The paved road for production-grade MCP Apps."
  tagline: "One CGo-free Go binary. Contract-first MCP servers and apps, with intrinsic observability, a local inspector, and one-command packaging."
  actions:
    - theme: brand
      text: Get started
      link: /getting-started/
    - theme: alt
      text: View on GitHub
      link: https://github.com/hurtener/dockyard

features:
  - title: Contract-first
    details: "Typed Go structs are the single source of truth. JSON Schema, TypeScript types, and fixtures are generated — server↔UI drift is caught by the toolchain, not by production users."
  - title: Observability built in
    details: "Every server emits Logbook — Dockyard's canonical event stream (wire identifier `obs/v1`). The inspector and any future console are pure clients of it. OpenTelemetry export is an optional adapter, never a prerequisite."
  - title: Server-side only
    details: "Harbor owns the MCP client. Dockyard ships no production client. The lone client-shaped component is the local inspector — dev-mode-gated, localhost-only, read-only."
  - title: Forward-compatible by isolation
    details: "Every MCP extension wire format lives behind one internal seam (`internal/protocolcodec`). A spec bump is regenerate-and-diff, not a code surgery."
  - title: One static binary
    details: "`dockyard build` produces a CGo-free statically-linked binary with the Svelte UI embedded. Cross-compile matrix with SHA-256 checksums on demand."
  - title: A real inspector
    details: "Drive your tools by hand, switch fixtures across UI states (happy/empty/error/permission/slow/large), render Apps in a sandboxed iframe, walk a task's lifecycle as a Timeline."
---

## Install

Dockyard is stable. Install the latest release with one command:

```bash
go install github.com/hurtener/dockyard/cmd/dockyard@latest
```

Full release notes are in [`CHANGELOG.md`](https://github.com/hurtener/dockyard/blob/main/CHANGELOG.md);
the post-V1 backlog (what comes next) lives in
[`docs/V2-BACKLOG.md`](https://github.com/hurtener/dockyard/blob/main/docs/V2-BACKLOG.md).

## Why Dockyard

Dockyard is the third product in a three-part ecosystem:

```text
Portico  — the MCP gateway        (connects and governs tools)
Harbor   — the agent framework    (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework (builds the MCP servers and apps users touch)
```

> Portico connects. Harbor reasons. Dockyard presents.

## The two templates

Two product-pattern templates ship with Dockyard, each exercising the framework
end to end.

### `analytics-widgets` — read-side

Three contract-first widget tools (`create_chart`, `create_table`,
`create_metric_card`) rendered inline by one Svelte App that composes the
shared `web/ui/` design system plus a `Sparkline` and an Apache-ECharts
`ChartFrame`. The canonical "render structured data inline in the host"
example.

![analytics-widgets chart](/screenshots/analytics-widgets/chart.png)

[Walk it through →](/getting-started/analytics-widgets)

### `approval-flows` — write-side

Two task-augmented tools (`request_approval`, `propose_with_edits`)
rendered as a human-in-the-loop card / form by one Svelte App that drives
approval before task creation through core MRTR or during durable work through
`tasks/update`. The canonical
Tasks × Apps showcase.

![approval-flows](/screenshots/phase-25/request-approval.png)

[Walk it through →](/getting-started/approval-flows)

## Built doc-driven

Dockyard is built with a doc-driven methodology — research briefs → RFC →
master phase plan → phased implementation — to keep design and code
coherent across a long build. The artifacts are in-repo and the docs
site renders them directly.

- [RFC-001](/reference/rfc) — design source of truth
- [Master phase plan](/reference/master-plan)
- [Decisions log](/reference/decisions)
- [Glossary](/reference/glossary)
- [Design conventions](/reference/design-conventions)
