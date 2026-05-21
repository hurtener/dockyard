// Package tasks is the Dockyard server-side MCP Tasks extension
// (io.modelcontextprotocol/tasks, experimental, SEP-1686/2663).
//
// # Why this package exists (RFC §8.1–§8.3; brief 02)
//
// The MCP Tasks extension turns a slow request into a durable handle the
// requestor polls and resumes, instead of blocking a connection until a
// transport timeout kills it. Dockyard V1 implements Tasks server-side: a
// task-augmented tools/call returns a CreateTaskResult immediately and the real
// CallToolResult is fetched later through tasks/result.
//
// # The shim (RFC §8.2)
//
// The official go-sdk has no released Tasks API, and its receiving-method
// dispatch is keyed on a fixed package-level map an unknown method
// (tasks/get, …) never reaches. Dockyard therefore routes tasks/* itself:
// [Engine] is a transport-agnostic JSON-RPC method router for the four Tasks
// methods. [Engine.Dispatch] takes a method name and raw params and returns
// raw result JSON, so the same engine serves any transport — the Phase 14
// transport mount, the inspector, an integration test. Every wire shape is
// encoded and decoded through internal/protocolcodec; this package constructs
// no raw extension wire JSON (binding property P3).
//
// # The five-status lifecycle (RFC §8.3)
//
// A task begins in working; legal transitions are working →
// {input_required, completed, failed, cancelled} and input_required →
// {working, completed, failed, cancelled}; the three terminal statuses are
// immutable. The Engine enforces every transition through the [TaskStore]
// seam; an illegal transition is a typed error ([ErrIllegalTransition]), never
// a panic across the MCP boundary.
//
// # The TaskStore seam (Phase 14)
//
// Durable task state lives behind the [TaskStore] interface. Phase 13 ships an
// in-memory stub ([NewInMemoryStore]) sufficient for stdio single-user apps and
// for tests; Phase 14 supplies the durable Store-backed driver carrying TTL
// enforcement, per-requestor concurrency caps, the purge sweep, crypto-strong
// task IDs and auth-context binding. The seam already carries the data those
// concerns need (TaskRecord.AuthContext, TaskRecord.RequestedTTL) so Phase 14
// enforces them without reshaping the interface.
package tasks
