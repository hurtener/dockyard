// Package inspector implements the host-side core of Dockyard's local
// inspector — the single test/debug surface for exercising an MCP server and
// its Apps without a real host (RFC §12).
//
// The inspector is the lone client-shaped component Dockyard ships, and it is
// kept narrowly so: it is dev-mode-gated, localhost-only, and read-only. This
// package enforces the localhost-only property mechanically — [New] refuses any
// non-loopback bind address with [ErrNonLoopbackBind], and the listener is
// never opened for a non-loopback address. The CVE-2025-49596 RCE in the
// official MCP Inspector's proxy is the cautionary tale (brief 05 §4.2): the
// inspector relays only what the UI needs, read-only, and is never an
// arbitrary-execution proxy.
//
// What this package builds:
//
//   - [Inspector] — a localhost HTTP server that serves the embedded
//     web/inspector frontend and relays the obs/v1 SSE stream and a JSON-RPC log
//     to it. It is a reusable concurrent artifact: many UI clients may connect
//     and disconnect concurrently.
//   - The obs/v1 relay — a pure SSE client of runtime/obs's SSE sink (P2: the
//     inspector consumes the public obs/v1 contract, it never reads runtime
//     internals).
//   - The read-only `/api/verdicts` and `/api/contracts` sources — the Verdicts
//     panel reuses internal/validate.Run (see [VerdictsFromValidate]), and the
//     fixture switcher derives its fixtures from the generated tool contracts
//     (P1). Both are optional [Options] fields; when unset the endpoints answer
//     with an empty array so the UI renders its four-state empty state.
//   - The host-half of the ui/ bridge, the fixture switcher, per-tool analytics,
//     capability-set emulation, and task-lifecycle rendering all live in the
//     web/inspector frontend; this package serves that frontend and the App
//     preview HTML, and is consumed by the `dockyard inspect` CLI verb.
//
// The inspector was built across Phase 22 (the core — the HTTP backend, the
// relay, the obs view) and Phase 23 (the advanced surface — verdicts,
// contracts, and the `dockyard inspect` command). It carries no production MCP
// client: `dockyard inspect` attaches the read-only relay to a server's obs/v1
// stream, never an MCP session (P4 — D-099).
package inspector
