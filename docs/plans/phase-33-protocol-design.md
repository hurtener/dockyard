# Phase 33 protocol design note

**Status:** Proposed for design-owner approval

**Researched:** 2026-07-11

**Implementation target:** MCP core draft `2026-07-28` plus Tasks extension
`io.modelcontextprotocol/tasks` at `29f83d5c8b34966d7795fb10046245f47c8d02c0`

## Sources

- MCP core draft `schema/draft/schema.ts` and Multi Round-Trip Requests page on
  the `2026-07-28` release branch.
- Tasks extension schema and specification at commit
  `29f83d5c8b34966d7795fb10046245f47c8d02c0` (2026-05-22).
- SEP-2322 (Multi Round-Trip Requests) and SEP-2663 (Tasks extension, Final).
- Go SDK `v1.7.0-pre.2`, including its MRTR types and custom-method API.

The core release and Tasks extension remain tentative until their final release
artifacts are pinned. Phase 31's finalization gate still applies.

## Finding: two input lifecycles

The tentative specifications define two related but distinct input lifecycles.
Dockyard must not combine their wire state.

### Core MRTR

Core MRTR applies to `tools/call`, `prompts/get`, and `resources/read`. A handler
may return an `InputRequiredResult` with `resultType: "input_required"` and at
least one of:

- `inputRequests`, a map whose values are `elicitation/create`,
  `sampling/createMessage`, or `roots/list` requests;
- `requestState`, an opaque string the client echoes on a new invocation of the
  original method alongside `inputResponses`.

Each retry is an independent request with a new JSON-RPC ID. A server may be
fully stateless by encoding its continuation in `requestState`. State that can
affect authorization, resource access, or business logic must have integrity
protection. The specification recommends binding it to the authenticated
principal, expiry, original method, and salient-parameter digest. Single-use
continuations still require durable replay protection.

The Go SDK already exposes this flow through `CallToolParams.InputResponses`,
`CallToolParams.RequestState`, and the corresponding result fields. Dockyard
must use those typed SDK fields at the standard method boundary; it must not
invent a custom MRTR method.

### Task mid-flight input

Task input begins only after a server has returned a `CreateTaskResult`. It does
not use `requestState` and does not retry the original method:

1. `tasks/get` returns a `DetailedTask` with `status: "input_required"` and all
   outstanding `inputRequests`.
2. The client submits matching `inputResponses` through `tasks/update`.
3. `tasks/update` returns an empty `resultType: "complete"` acknowledgement.
4. The client resumes polling with `tasks/get`.

Request keys are unique for the lifetime of a task. Unknown, duplicate, or
already-satisfied keys should be ignored. Partial updates may be accepted, in
which case the task remains `input_required`.

For a tool that needs input before durable asynchronous work starts, the Tasks
spec recommends completing core MRTR first and returning `CreateTaskResult`
afterward. The two flows have independent request-key scopes.

## Modern Tasks wire contract

The modern extension has these properties:

- Capability identifier: `io.modelcontextprotocol/tasks`, advertised as `{}` in
  per-request client capabilities and `server/discover` server capabilities.
- Task-augmented method: `tools/call` only.
- Methods: `tasks/get`, `tasks/update`, and `tasks/cancel`.
- Removed methods: `tasks/result` and `tasks/list`.
- `CreateTaskResult` is flat and includes `resultType: "task"`.
- `Task` uses `ttlMs` and `pollIntervalMs`.
- `tasks/get` returns a flat status-specific `DetailedTask` with
  `resultType: "complete"`.
- `tasks/update` and `tasks/cancel` return empty acknowledgements with
  `resultType: "complete"`.
- Unknown task IDs produce `-32602`; modern `tasks/list` and `tasks/result`
  produce `-32601`.
- Every task request is independently authenticated and authorized.
- HTTP clients set `Mcp-Method` and set `Mcp-Name` to `params.taskId`.
- Task IDs remain crypto-strong explicit handles; no session or connection
  identity is implied.

Modern Tasks is not wire-compatible with the `2025-11-25` experimental shape.
The legacy codec retains `tasks/result`, conditionally scoped `tasks/list`, task
request augmentation, initialize capability rewriting, and
`dockyard/tasks/supplyInput`. None of those fields or methods may be emitted by
the modern codec.

## SDK integration

The pinned SDK provides the seam required by Phase 33:

```go
mcp.AddReceivingCustomMethod(server, method, handler)
```

Modern `tasks/get`, `tasks/update`, and `tasks/cancel` should register through
that seam so they participate in SDK decoding, middleware, per-request metadata,
and stateless transport handling. The legacy raw-frame mount remains only on the
legacy transport path.

The SDK deliberately rejects server-initiated requests in the modern lifecycle.
Both MRTR and task input therefore carry request objects as data for the client
to execute; Dockyard must not call SDK server-to-client request APIs while
processing a modern request.

## Proposed Dockyard handler model

Keep task and MRTR APIs separate:

```go
type InputRequest struct {
	Key     string
	Method  InputMethod
	Payload json.RawMessage
}

func (h *TaskHandle) RequestInput(ctx context.Context, req InputRequest) error
```

`TaskHandle.RequestInput` persists an outstanding task input request, moves the
task to `input_required`, and returns control to the task worker. It is resumed
only after `tasks/update` durably records matching input. It does not create or
consume core `requestState`.

Core MRTR should use a separate server/tool continuation API built around the
SDK's typed `InputRequests`, `InputResponses`, and opaque string
`RequestState`. The concrete app-facing continuation API belongs to the server
response-semantics work shared with Phase 34; Phase 33 only needs enough support
to exercise approval before task creation.

## Approval-flow migration

The approval template should exercise both standards-based choices explicitly:

- approval required before creating durable work: core MRTR retry of
  `tools/call`;
- approval required during durable work: `tasks/get` input request followed by
  `tasks/update`.

The existing inspector relay to `dockyard/tasks/supplyInput` remains available
only when inspecting a legacy peer. A modern inspector acts as the client: it
fulfills MRTR input and retries the original call, or sends `tasks/update` for a
task input request.

## Security and persistence

- Derive the authenticated principal on every modern request and compare it to
  the task's persisted authorization binding.
- Treat task IDs and MRTR state as attacker-controlled inputs even when they are
  unguessable or authenticated.
- Persist task input requests and accepted responses; a suspended goroutine or
  process-local channel cannot be the source of truth.
- Protect MRTR request state with AEAD or HMAC when it carries trusted state.
- Bind protected state to principal, expiry, method, and salient arguments.
- Enforce server-side single use where replay could duplicate a side effect.

## Implementation sequence

1. Replace the incomplete Tasks extract with the full pinned upstream schema
   and pin the matching normative prose.
2. Add a separate modern codec; leave the legacy codec behavior unchanged.
3. Register modern task methods through `AddReceivingCustomMethod` and advertise
   Apps and Tasks through `server/discover`.
4. Persist outstanding task input and implement `tasks/update` authorization and
   idempotency.
5. Add core MRTR support at the tool boundary using SDK fields.
6. Migrate the inspector and approval template, preserving an explicit legacy
   adapter.
7. Add modern/legacy wire goldens, fuzz tests, real HTTP integration tests, and
   concurrent-store tests.

## Approval questions

Before implementation, the design owner must approve:

1. The strict separation between core MRTR continuation state and task input.
2. The persisted `TaskHandle.RequestInput` model and its response-consumption
   semantics.
3. Whether Phase 33 owns the minimal core MRTR app API or Phase 34 supplies it
   behind a temporary internal seam.
4. Whether modern task notifications are implemented now or deferred while
   polling remains fully conformant.
