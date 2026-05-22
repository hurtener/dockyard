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
//   - The read-only `/api/apps` source — [AppsFromServer] renders the attached
//     server's ui:// Apps by a read-only resources/list + resources/read of the
//     server (RFC §12 line 711 — the inspector renders the server's Apps).
//   - The host-half of the ui/ bridge, the fixture switcher, per-tool analytics,
//     capability-set emulation, and task-lifecycle rendering all live in the
//     web/inspector frontend; this package serves that frontend and the App
//     preview HTML, and is consumed by the `dockyard inspect` CLI verb.
//
// The inspector was built across Phase 22 (the core — the HTTP backend, the
// relay, the obs view) and Phase 23 (the advanced surface — verdicts,
// contracts, and the `dockyard inspect` command); remediation R1 wired the
// shipping `dockyard inspect` to the verdicts, contracts, and App-preview
// sources that were previously only test-reachable.
//
// The inspector is not a production MCP client. It performs exactly two
// client-shaped operations, both read-only: it relays a server's obs/v1 SSE
// stream, and — to render the server's Apps — it performs a read-only
// resources/list + resources/read of the server's ui:// resources (D-103,
// which extends D-099). It never issues a mutating MCP call, stays dev-gated
// and localhost-only, and is never an arbitrary-execution proxy (P4).
package inspector
