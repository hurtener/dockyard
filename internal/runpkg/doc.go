// Package runpkg implements the `dockyard run` verb — running a Dockyard
// project's MCP server on a chosen transport (RFC §14, §9.1).
//
// `dockyard run --transport <stdio|http>` builds the project (reusing the
// internal/buildpkg pipeline — a host-only build) and then runs the produced
// server binary as a supervised child process. The transport selection and
// the HTTP listen address are passed to the child through the
// DOCKYARD_TRANSPORT and DOCKYARD_HTTP_ADDR environment variables; the
// project's own main.go owns its transport wiring (RFC §5.2 — runpkg drives
// the server, it never reimplements a transport).
//
// runpkg honours context cancellation / SIGINT: cancelling the context tears
// the server child down cleanly with a SIGTERM-then-SIGKILL grace window, the
// same teardown discipline internal/devloop uses, so a `dockyard run` leaves
// no orphan process.
//
// runpkg is internal — the reusable, testable seam the `dockyard run` cobra
// verb and the Phase 20 integration test consume. It holds no shared mutable
// state.
package runpkg
