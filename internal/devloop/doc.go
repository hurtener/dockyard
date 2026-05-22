// Package devloop is the embedded fsnotify dev orchestrator behind the
// `dockyard dev` command (RFC §9.2).
//
// One `dockyard dev` process supervises a process tree from a single command.
// devloop embeds an fsnotify file watcher — Dockyard does not shell out to
// air or wgo (RFC §9.2, brief 06 §2.6) — and choreographs three concerns:
//
//   - the Go MCP server, rebuilt and restarted on a .go source change (Go has
//     no in-process hot reload, so the server is restarted, not patched);
//   - codegen, re-run in-process via internal/generate on a contract-source
//     change, so the generated types are live before the server restarts;
//   - the Vite dev server, started and supervised for the project's web/ UI
//     (Vite owns Svelte HMR — Dockyard never reimplements it).
//
// The orchestrator is a reusable, concurrency-safe artifact: Run is the single
// public entrypoint, the `dockyard dev` cobra verb is a thin wrapper over it,
// and the integration test drives the same Run. It holds no global state.
//
// Lifecycle. Run blocks until the supplied context is cancelled (Ctrl-C) or a
// fatal error occurs. On return, the whole process tree — the Go server, Vite,
// and the watcher — is torn down: no orphan processes, no leaked goroutines,
// no leaked ports. A child-process crash is reported through log/slog and the
// loop survives; devloop never panics across the boundary (CLAUDE.md §5, §13).
//
// Graceful degradation. A scaffolded blank server has no web/ UI project; in
// that case devloop supervises only the Go server, logs that no UI project was
// found, and does not error (RFC §4.1: a UI resource is additive).
package devloop
