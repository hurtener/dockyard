// Package inspector implements the host-side core of Dockyard's local
// inspector — the single test/debug surface for exercising an MCP server and
// its Apps without a real host (RFC §12).
//
// The inspector is the lone client-shaped component Dockyard ships, and it is
// kept narrowly so: it is dev-mode-gated, localhost-only, and
// **operator-initiated only** — every mutating call the inspector issues
// happens as the direct result of an operator's deliberate UI action through
// the localhost-bound listener. Phase 27's security re-audit re-cast the
// pre-existing "read-only" framing as **operator-initiated only** to match
// the surface that grew through D-131 (operator-initiated `tools/call`) and
// D-134 (operator-initiated elicitation `tasks/result`); the older
// "read-only" wording was honest before those decisions and overpromised
// after them. The new framing remains within P4: the inspector is the lone
// client-shaped component, dev-mode-gated, localhost-bound, and never an
// arbitrary-execution proxy — and every mutating call has a named operator
// trigger, an audited code path, and a corresponding decision entry.
//
// This package enforces the localhost-only property mechanically — [New]
// refuses any non-loopback bind address with [ErrNonLoopbackBind], and the
// listener is never opened for a non-loopback address. The CVE-2025-49596 RCE
// in the official MCP Inspector's proxy is the cautionary tale (brief 05
// §4.2): the inspector relays only what the UI needs, and is never an
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
//   - The operator-initiated `/api/tools/invoke` surface — [ToolsFromServer]
//     opens a short-lived MCP client session, calls `tools/call` once, and
//     closes (D-131). The operator triggers this through the UI's Invoke
//     button; no other actor can.
//   - The operator-initiated `/api/tasks/elicitation` surface —
//     [ElicitationFromServer] posts a single `tasks/result` JSON-RPC frame to
//     deliver an App's elicitation reply (D-134). The operator triggers this
//     through the App preview's Approve / Reject button; no other actor can.
//   - The host-half of the ui/ bridge, the fixture switcher, per-tool analytics,
//     capability-set emulation, and task-lifecycle rendering all live in the
//     web/inspector frontend; this package serves that frontend and the App
//     preview HTML, and is consumed by the `dockyard inspect` CLI verb.
//
// The inspector was built across Phase 22 (the core — the HTTP backend, the
// relay, the obs view), Phase 23 (the advanced surface — verdicts, contracts,
// and the `dockyard inspect` command), Phase 24's finishing pass (D-131
// operator-initiated `tools/call`), and Phase 25 (D-134 operator-initiated
// elicitation forwarding). Phase 27's security re-audit captures the
// production `mcp.NewClient` set (this package's [AppsFromServer] +
// [ToolsFromServer] + [ElicitationFromServer], plus `internal/installpkg`'s
// boot check D-088) in `test/integration/phase27_inspector_security_test.go`;
// any new mcp.NewClient call site outside that set is a P4 violation that
// fails the audit before it can merge.
//
// The inspector is not a production MCP client. It performs four operator-
// initiated client-shaped operations: it relays a server's obs/v1 SSE stream
// (read-only); it renders the server's Apps via a read-only `resources/list +
// resources/read` of the server's ui:// resources (D-103); it issues a
// short-lived `tools/call` when the operator clicks Invoke (D-131); and it
// posts a `tasks/result` frame when the operator clicks Approve / Reject
// inside an App's elicitation prompt (D-134). It stays dev-gated and
// localhost-only, never holds a long-lived client, and is never an
// arbitrary-execution proxy (P4).
package inspector
