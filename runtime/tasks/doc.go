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
// # The TaskStore seam
//
// Durable task state lives behind the [TaskStore] interface. [NewInMemoryStore]
// is the in-memory driver, sufficient for stdio single-user apps and for tests;
// [NewStore] is the durable driver — a typed facade over the runtime/store
// seam, with a forward-only migration (D-070). Both pass the shared TaskStore
// conformance suite (runtime/tasks/taskstoretest).
//
// # The TaskHandle handler API (RFC §8.4)
//
// A handler doing genuinely long work takes a [HandleFunc] and receives a
// [TaskHandle]: progress reporting, status messages, cooperative cancellation,
// and input_required-driven elicitation. Handlers stay sync-shaped — the handle
// is how a sync-shaped handler does long async-feeling work. No raw experimental
// protocol struct reaches the handle (P3).
//
// # Lifecycle controls and security (RFC §8.5)
//
// The [Lifecycle] options — manifest-tunable max TTL, default TTL, per-requestor
// concurrency cap and a background TTL purge sweep — bound durable task state.
// Task IDs are crypto-strong (128-bit crypto/rand, [CryptoID]); [Engine.DispatchAs]
// binds tasks/get|result|cancel to the requestor's authorization context and
// scopes tasks/list to the caller, withholding it when requestors are not
// identifiable.
//
// # The transport mount (RFC §8.2)
//
// [Mount] routes tasks/* JSON-RPC frames into [Engine.Dispatch] ahead of the SDK
// server — the go-sdk rejects unknown methods before middleware — and injects
// the capabilities.tasks block into the initialize handshake, so a real MCP
// client drives tasks/* end to end over a transport (D-071).
package tasks
