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
// What this package builds (Phase 22 — the inspector core):
//
//   - [Inspector] — a localhost HTTP server that serves the embedded
//     web/inspector frontend and relays the obs/v1 SSE stream and a JSON-RPC log
//     to it. It is a reusable concurrent artifact: many UI clients may connect
//     and disconnect concurrently.
//   - The obs/v1 relay — a pure SSE client of runtime/obs's SSE sink (P2: the
//     inspector consumes the public obs/v1 contract, it never reads runtime
//     internals).
//   - The host-half of the ui/ bridge lives in the web/inspector frontend; this
//     package serves that frontend and the App preview HTML.
//
// Out of scope for Phase 22 (deferred to Phase 23): the fixture switcher,
// per-tool analytics, contract-drift/spec-compliance verdicts, capability-set
// emulation, task-lifecycle rendering, and the standalone `dockyard inspect`
// command. This package leaves clean seams for them.
package inspector
