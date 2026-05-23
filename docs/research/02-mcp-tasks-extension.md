# Brief 02 — MCP Tasks extension

**Date:** 2026-05-20
**Sources:**

- https://modelcontextprotocol.io/extensions/tasks/overview — reachable (overview page; note: lags the spec — uses `tasks/update`, which the authoritative spec does **not** define)
- https://github.com/modelcontextprotocol/experimental-ext-tasks — reachable; mined via GitHub API. Authoritative files: `docs/specification/draft/tasks.mdx`, `schema/draft/schema.ts`, `seps/1686-tasks.md`
- https://github.com/modelcontextprotocol/go-sdk — reachable; no confirmed Tasks-extension surface as of 2026-05 (see §4)

**Status:** Draft for RFC-001-Dockyard

> **Editor's note (Phase 25, 2026-05-23):** This brief refers to the human-in-
> the-loop template as `approval-flow` (singular). V1 ships it as
> **`approval-flows`** (plural) — the rename was approved in Phase 25 (see
> `docs/plans/phase-25-approval-flows.md`). The historical references below are
> preserved verbatim: research briefs are *context*, not *design*, and rewriting
> a brief's findings is forbidden (`AGENTS.md` §16). When you see `approval-flow`
> below, read it as the V1 template `approval-flows`.

---

## 1. Why this brief exists

Dockyard V1 ships the **MCP Tasks extension** server-side, alongside MCP Apps, as a settled
decision. Tasks is the protocol primitive that turns a slow tool call into a *durable handle*
the host can poll and resume — exactly the contract behind the braindump's `task-runner`
template (data sync, report generation, batch processing, agent execution) and the
`approval-flow` template (human-in-the-loop gates). Without Tasks, a long-running Dockyard
tool either blocks a connection until a transport timeout kills it, or invents a bespoke
job-ID convention every host has to special-case.

This brief pins down the *current* protocol surface so RFC-001 can scope what the Dockyard
server runtime must implement. The Tasks extension is **experimental** — the repo itself
warns it "may change significantly or be discontinued" — so this brief also isolates the
volatile parts behind which Dockyard must keep an abstraction seam.

## 2. Findings

### 2.1 What Tasks is

The Tasks extension (`io.modelcontextprotocol/tasks`) lets a **requestor** *augment* a normal
request so the **receiver** returns a durable **task** — a state machine carrying execution
status — instead of blocking for the final result. Tasks were introduced in MCP spec version
**2025-11-25** and are extracted into the `experimental-ext-tasks` repo for incubation; they
track **SEP-1686 / SEP-2663**.

Tasks is *direction-agnostic*: either side can be requestor or receiver. For Dockyard
(server-side only) the relevant directions are:

- **Server as receiver** of task-augmented `tools/call` — the primary case.
- **Server as requestor** of task-augmented `sampling/createMessage` and `elicitation/create`
  toward the client (needed when a long task elicits user input mid-flight).

### 2.2 Lifecycle / status model

Five statuses (`TaskStatus`): `working`, `input_required`, `completed`, `failed`, `cancelled`.

- A task **MUST** begin in `working`.
- Legal transitions: `working` → {`input_required`, `completed`, `failed`, `cancelled`};
  `input_required` → {`working`, `completed`, `failed`, `cancelled`}.
- `completed`, `failed`, `cancelled` are **terminal** and immutable once reached.
- `failed` is reached on a JSON-RPC error during execution **and** — for `tools/call`
  specifically — when the `CallToolResult` has `isError: true`.

### 2.3 Protocol surface (authoritative — from `schema/draft/schema.ts` + `tasks.mdx`)

**Augmenting a request.** The requestor adds a `task` field to request params:
`{ "task": { "ttl"?: number } }` (`TaskMetadata`; `ttl` in ms, optional, requested lifetime).

**`CreateTaskResult`** — returned *instead of* the normal result when a request is accepted as
a task. Shape: `{ "task": Task }`. (The overview page's `resultType: "task"` discriminator is
**not** in the authoritative schema — `CreateTaskResult` simply wraps a `task` object.)

**`Task` object fields:** `taskId` (string), `status` (`TaskStatus`), `statusMessage?`
(string), `createdAt` (ISO-8601), `lastUpdatedAt` (ISO-8601), `ttl` (`number | null`;
`null` = unlimited), `pollInterval?` (ms).

**JSON-RPC methods (receiver must serve):**

| Method | Params | Result | Notes |
|---|---|---|---|
| `tasks/get` | `{ taskId }` | `Result & Task` | Poll status. **Non-blocking** — returns current state immediately. |
| `tasks/result` | `{ taskId }` | the underlying request's result shape (e.g. `CallToolResult`) | **Blocks** until terminal. On `failed`, returns the same JSON-RPC error the request would have. |
| `tasks/cancel` | `{ taskId }` | `Result & Task` | Receiver **MUST** transition to `cancelled` before responding. |
| `tasks/list` | `PaginatedRequest` (`cursor?`) | `{ tasks: Task[], nextCursor? }` | Cursor-paginated. |

**Notification:** `notifications/tasks/status` — params are the full `Task`. Optional;
requestors **MUST NOT** rely on it.

> **Naming correction:** the overview page describes a `tasks/update` method for submitting
> mid-flight input and an `inputRequests`/`inputResponses` map. The authoritative spec and
> schema have **no `tasks/update`**. Mid-flight input is handled differently — see §2.5.

### 2.4 Polling vs. streaming

- **Polling is the default and the contract of record.** Requestors call `tasks/get`,
  respecting the server-suggested `pollInterval` (ms), until terminal.
- **Streaming is best-effort.** A server *may* emit `notifications/tasks/status` (full task
  state, no extra round-trip). Progress notifications from the core `progress` utility also
  work: the `progressToken` from the original request stays valid for the task's whole life.
- **Streamable HTTP nuance:** servers **SHOULD NOT** upgrade a `tasks/get` to an SSE stream
  (the client signalled it wants to poll); they *may* hold an SSE stream open on
  `tasks/result`. Clients may disconnect SSE at any time and resume polling.

### 2.5 Mid-flight input (`input_required`)

When a long task needs the user (e.g. an approval), the receiver moves it to `input_required`.
The requestor, on seeing that status, **SHOULD** call `tasks/result` — which *blocks* and is
the channel over which the receiver sends the actual `elicitation/create` (or
`sampling/createMessage`) request. That nested request **MUST** carry the
`io.modelcontextprotocol/related-task` `_meta` key tying it to the task. The requestor answers
the elicitation, the task transitions back to `working`, and polling resumes. There is no
separate `tasks/update` call.

### 2.6 Capability negotiation

Declared in the `tasks` capability at initialization. **Server capabilities:**

```json
{ "capabilities": { "tasks": {
  "list": {}, "cancel": {},
  "requests": { "tools": { "call": {} } } } } }
```

- `tasks.list` / `tasks.cancel` — gate those operations.
- `tasks.requests.tools.call` — server accepts task-augmented `tools/call`. This set is
  **exhaustive**: absent request type = unsupported.
- Client side advertises `tasks.requests.sampling.createMessage` /
  `tasks.requests.elicitation.create` — Dockyard, as requestor of those, must check them
  before augmenting.

**Tool-level negotiation (second, finer layer).** Each tool in `tools/list` declares
`execution.taskSupport`: `"forbidden"` (default if absent), `"optional"`, or `"required"`.

- `forbidden` → client must not task-augment; server **SHOULD** return `-32601` if it tries.
- `optional` → client may call either way.
- `required` → client **MUST** task-augment; server **MUST** return `-32601` otherwise.
This is gated *behind* `tasks.requests.tools.call` — if the capability is absent,
`taskSupport` is irrelevant.

### 2.7 Relationship to long-running tool calls and to the Apps extension

- **Long-running tool calls:** Tasks *is* the sanctioned mechanism. A Dockyard tool whose
  handler is slow declares `taskSupport: "optional"` (or `"required"`), and the runtime
  returns a `CreateTaskResult` rather than blocking. `tasks/result` later yields the exact
  `CallToolResult` the synchronous path would have produced.
- **Apps extension:** orthogonal but composable. An MCP App's `task-runner` template is a
  UI surface bound to a tool that returns a task; the App polls `tasks/get` (or consumes
  `notifications/tasks/status`) to render progress, logs, and cancel/retry actions. The
  `io.modelcontextprotocol/model-immediate-response` `_meta` key on `CreateTaskResult`
  (provisional, non-binding) lets the server hand the model a placeholder string so the host
  can return control while the App shows live status. Apps + Tasks together are the braindump's
  `task-runner` and `approval-flow` patterns.

### 2.8 Server obligations summary

To support Tasks, a server must: advertise the `tasks` capability; check the client declared
it before ever returning a `CreateTaskResult`; durably create the task *before* responding;
generate unique receiver-side task IDs; serve `tasks/get`/`tasks/result`/`tasks/cancel`
(and `tasks/list` if advertised); maintain `createdAt`/`lastUpdatedAt`; honor or override
`ttl` and report the actual value; enforce legal state transitions; bind tasks to auth
context (or document the absence and use crypto-strong IDs); and stamp
`io.modelcontextprotocol/related-task` on every task-related message except the `taskId`-bearing
RPCs themselves.

## 3. Go-flavored shapes / API sketches

These mirror the schema; final field names should be code-generated from the upstream schema
(see §5), not hand-maintained.

```go
// Package dyt — Dockyard Tasks runtime types (mirror of io.modelcontextprotocol/tasks).
type TaskStatus string

const (
	StatusWorking       TaskStatus = "working"
	StatusInputRequired TaskStatus = "input_required"
	StatusCompleted     TaskStatus = "completed"
	StatusFailed        TaskStatus = "failed"
	StatusCancelled     TaskStatus = "cancelled"
)

type Task struct {
	TaskID        string     `json:"taskId"`
	Status        TaskStatus `json:"status"`
	StatusMessage string     `json:"statusMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`      // marshals ISO-8601
	LastUpdatedAt time.Time  `json:"lastUpdatedAt"`
	TTL           *int64     `json:"ttl"`            // ms; nil => unlimited (never omitempty)
	PollInterval  *int64     `json:"pollInterval,omitempty"`
}
```

Dockyard's paved-road API should keep tool handlers task-agnostic and let the runtime decide.
The handler returns the normal output; opting into Tasks is a *registration-time* declaration:

```go
app.Tool("generate_report").
	Input[GenerateReportInput]().
	Output[GenerateReportOutput]().
	TaskSupport(dockyard.TaskOptional).        // -> execution.taskSupport in tools/list
	Handler(handleGenerateReport)              // handler stays sync-shaped
```

For genuinely long work, a richer handler receives a task controller so it can report
progress and observe cooperative cancellation:

```go
func handleGenerateReport(ctx context.Context, in GenerateReportInput, t dockyard.TaskHandle) (GenerateReportOutput, error) {
	t.SetStatusMessage("collecting rows")
	t.Progress(0.25)                           // -> notifications/progress on the original token
	if needsApproval {
		// runtime moves task to input_required, elicits over tasks/result channel
		ok, err := t.Elicit(ctx, approvalSchema)
		if err != nil { return GenerateReportOutput{}, err }
		_ = ok
	}
	select {
	case <-ctx.Done():                          // tasks/cancel cancels this ctx (cooperative)
		return GenerateReportOutput{}, ctx.Err()
	default:
	}
	return out, nil
}
```

The runtime owns a **TaskStore** interface so persistence is pluggable (in-memory for stdio
single-user apps; durable for HTTP/Portico-managed apps):

```go
type TaskStore interface {
	Create(ctx context.Context, t *Task, authCtx AuthContext) error
	Get(ctx context.Context, id string, authCtx AuthContext) (*Task, error)
	Transition(ctx context.Context, id string, to TaskStatus, msg string) error
	SetResult(ctx context.Context, id string, result json.RawMessage) error // or a JSON-RPC error
	List(ctx context.Context, cursor string, authCtx AuthContext) ([]*Task, string, error)
	Purge(ctx context.Context, now time.Time) (int, error)                  // TTL sweep
}
```

## 4. Sharp edges & risks

1. **Spec vs. overview page divergence.** The `/extensions/tasks/overview` page documents
   `tasks/update` + `inputRequests`/`inputResponses`; the authoritative spec and schema do
   **not**. Building against the overview page would produce a non-compliant server. Dockyard
   must build against `experimental-ext-tasks/schema/draft/schema.ts` and `tasks.mdx`.
2. **Experimental status.** The repo explicitly states the extension "is not an official
   extension and may change significantly or be discontinued." Method names, the
   `CreateTaskResult` shape, and `model-immediate-response` are all flagged provisional.
   Dockyard must keep Tasks behind a versioned internal interface and a code-generated wire
   layer, never leaking raw protocol structs into handler-facing APIs.
3. **go-sdk gap.** As of 2026-05 the official `modelcontextprotocol/go-sdk` confirms tools,
   resources, prompts, sampling, elicitation — **no confirmed Tasks surface**. Dockyard will
   likely have to implement Tasks method routing (`tasks/*`) and capability advertisement
   *on top of* go-sdk, possibly via a custom method handler, until upstream lands it. This is
   a real V1 build item, not a config flag.
4. **`tasks/result` blocking semantics under stdio/HTTP.** `tasks/result` blocks until
   terminal; it is also the channel for `input_required` elicitations. The runtime must
   service it on a goroutine that can both wait on the task *and* push a nested
   `elicitation/create` — without deadlocking the single stdio pipe. Streamable HTTP adds SSE
   stream-lifecycle subtleties (don't upgrade `tasks/get`; clients may disconnect anytime).
5. **Security: ID guessing.** Without auth context, task IDs are the only access control.
   Dockyard's default ID generator must be crypto-strong (e.g. 128-bit random), and the
   runtime **MUST NOT** advertise `tasks.list` when it cannot identify requestors. With auth
   context, `tasks/get|result|cancel` must reject cross-context access and `tasks/list` must
   scope to the caller.
6. **TTL / resource exhaustion.** Tasks are durable state; a leak is an unbounded memory or
   DB growth. The runtime needs an enforced max TTL, a per-requestor concurrent-task cap, and
   a background purge sweep — all surfaced as manifest-level config.
7. **`cancelled` is cooperative.** A cancelled task may still run to completion underneath;
   it must stay `cancelled`. Handler code must treat `ctx` cancellation as advisory and the
   runtime must ignore late terminal transitions on already-cancelled tasks.

## 5. What Dockyard must adopt / build / avoid

### Adopt

- Treat `experimental-ext-tasks` `schema/draft/schema.ts` as the single source of truth;
  the `/overview` page is a non-authoritative summary.
- The two-layer negotiation model: `tasks` capability *and* per-tool `execution.taskSupport`.
- Polling-as-default; `notifications/tasks/status` and `progress` as best-effort extras.

### Build (server-side, V1)

- A **Tasks runtime** layered over go-sdk: capability advertisement; `tasks/*` method
  routing; `CreateTaskResult` substitution for task-augmented `tools/call`.
- A pluggable **TaskStore** (in-memory default; durable adapter for HTTP/Portico modes) with
  TTL sweeper and per-requestor concurrency limits.
- A `TaskHandle` handler API exposing progress, `statusMessage`, cooperative cancellation,
  and `input_required`-driven elicitation — keeping handlers sync-shaped.
- Manifest knobs: per-tool `taskSupport`, default/max `ttl`, default `pollInterval`,
  concurrency cap.
- Code generation of wire types from the upstream schema, isolated in one package so a spec
  bump is a regenerate-and-diff, not a refactor.
- Inspector support: render task lifecycle, poll timeline, and `input_required` round-trips
  so developers can debug Tasks locally (DX-is-king).
- Auth-context binding of tasks; crypto-strong ID generation; `tasks.list` gated on
  identifiability.

### Avoid

- Implementing `tasks/update` / `inputRequests` from the overview page — not in the spec.
- Leaking experimental protocol structs into the public handler API — wrap everything.
- Advertising `tasks.list` in unauthenticated single-user stdio mode.
- Holding connections open instead of returning tasks — the failure mode Tasks exists to fix.
- A `resultType` discriminator on `CreateTaskResult` — the schema wraps a `task` object, no tag.

## 6. Open questions

- **Q-1.** Does Dockyard implement `tasks/*` routing itself on top of `go-sdk`, or wait for /
  upstream a go-sdk Tasks contribution? V1 timeline likely forces the former — confirm.
- **Q-2.** Default `TaskStore` for HTTP/Portico-managed apps — embedded (SQLite/bbolt, no CGo
  rules out cgo-SQLite) vs. pluggable-only with no bundled durable default?
- **Q-3.** Should every Dockyard `task-runner`-template tool default to
  `taskSupport: "required"`, or `"optional"` to preserve a synchronous fast path?
- **Q-4.** How does Dockyard pin against an experimental spec — vendor a schema snapshot per
  Dockyard release and gate on the negotiated MCP protocol version?
- **Q-5.** Should Dockyard emit `notifications/tasks/status` by default (better App UX, more
  traffic) or opt-in per tool?
- **Q-6.** Does Dockyard set `io.modelcontextprotocol/model-immediate-response` by default
  for App-bound task tools, given its provisional/non-binding status?
- **Q-7.** Cancellation propagation contract: does `tasks/cancel` always cancel the handler
  `context.Context`, and what is the runtime's policy on late terminal transitions?
- **Q-8.** Concurrency-cap and max-TTL defaults — what values, and are they manifest-tunable
  per app or framework-fixed?

## 7. Sources

- MCP Tasks — Overview: https://modelcontextprotocol.io/extensions/tasks/overview (reachable; non-authoritative, lags the spec)
- `experimental-ext-tasks` repo: https://github.com/modelcontextprotocol/experimental-ext-tasks (reachable)
  - Spec: `docs/specification/draft/tasks.mdx`
  - Schema (source of truth): `schema/draft/schema.ts`
  - Proposal: `seps/1686-tasks.md` (SEP-1686 / SEP-2663)
- MCP core spec 2025-11-25, Tasks utility: https://modelcontextprotocol.io/specification/2025-11-25
- Official Go SDK: https://github.com/modelcontextprotocol/go-sdk and https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp (reachable; no confirmed Tasks surface as of 2026-05)
