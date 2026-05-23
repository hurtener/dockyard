package protocolcodec

import (
	"encoding/json"
	"time"
)

// This file holds the Dockyard domain types for the MCP Tasks extension
// (io.modelcontextprotocol/tasks, experimental) and the JSON shapes used to
// (un)marshal the parts of it that ride in `_meta`.
// Spec snapshot: docs/specifications/mcp-tasks-experimental.schema.ts
// (SEP-1686/2663). The schema is experimental; isolating it here is exactly
// why the seam exists (RFC §8.2, D-010; brief 02 §4 risk 2).

// TaskStatus is the MCP Tasks lifecycle status. The five values and their
// legal transitions are fixed by the schema (brief 02 §2.2).
type TaskStatus string

// The five task statuses. A task MUST begin in [TaskWorking]; [TaskCompleted],
// [TaskFailed] and [TaskCancelled] are terminal and immutable.
const (
	TaskWorking       TaskStatus = "working"
	TaskInputRequired TaskStatus = "input_required"
	TaskCompleted     TaskStatus = "completed"
	TaskFailed        TaskStatus = "failed"
	TaskCancelled     TaskStatus = "cancelled"
)

// IsTerminal reports whether s is a terminal task status.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled:
		return true
	default:
		return false
	}
}

// Valid reports whether s is one of the five spec-defined statuses.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskWorking, TaskInputRequired, TaskCompleted, TaskFailed, TaskCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo reports whether a task may move from status s to status to,
// per the schema's transition rules: working → {input_required, completed,
// failed, cancelled}; input_required → {working, completed, failed,
// cancelled}; terminal states have no outgoing transitions (brief 02 §2.2).
func (s TaskStatus) CanTransitionTo(to TaskStatus) bool {
	if !s.Valid() || !to.Valid() {
		return false
	}
	switch s {
	case TaskWorking:
		return to == TaskInputRequired || to.IsTerminal()
	case TaskInputRequired:
		return to == TaskWorking || to.IsTerminal()
	default: // terminal — immutable
		return false
	}
}

// ToolTaskSupport is a tool's declared relationship to the Tasks extension,
// surfaced as `execution.taskSupport` in `tools/list` (brief 02 §2.6).
type ToolTaskSupport string

// The three tool task-support values. Absent on the wire defaults to
// [TaskSupportForbidden].
const (
	TaskSupportForbidden ToolTaskSupport = "forbidden"
	TaskSupportOptional  ToolTaskSupport = "optional"
	TaskSupportRequired  ToolTaskSupport = "required"
)

// Valid reports whether t is one of the three spec-defined values.
func (t ToolTaskSupport) Valid() bool {
	switch t {
	case TaskSupportForbidden, TaskSupportOptional, TaskSupportRequired:
		return true
	default:
		return false
	}
}

// Task is the Dockyard domain view of the Tasks `Task` object — the durable
// state machine returned inside a `CreateTaskResult` and by `tasks/get` /
// `tasks/cancel` (brief 02 §2.3).
type Task struct {
	// ID is the receiver-side task identifier (`taskId`).
	ID string
	// Status is the current lifecycle status.
	Status TaskStatus
	// StatusMessage is an optional human-readable status description.
	StatusMessage string
	// CreatedAt / LastUpdatedAt are ISO-8601 timestamps on the wire.
	CreatedAt     time.Time
	LastUpdatedAt time.Time
	// TTL is the actual retention duration in milliseconds; a nil pointer
	// encodes the schema's `null` (unlimited retention). Per schema this field
	// is always present on the wire — never omitted — so nil marshals to JSON
	// `null`, not an absent key.
	TTL *int64
	// PollInterval is the receiver's suggested polling interval in
	// milliseconds; a nil pointer omits it.
	PollInterval *int64
}

// TaskMeta is the Dockyard domain view of the request-augmentation metadata —
// the `task` field a requestor adds to request params (`TaskMetadata`,
// brief 02 §2.3). It is NOT a `_meta` key; it is a top-level request param.
// It is carried here so the whole Tasks wire surface lives behind one seam.
type TaskMeta struct {
	// TTL is the requested retention duration in milliseconds; a nil pointer
	// omits it (the requestor expresses no preference).
	TTL *int64
}

// CreateTaskResultMeta is the Dockyard domain view of the optional `_meta`
// keys carried on a `CreateTaskResult` — currently just the provisional
// model-immediate-response string (brief 02 §2.7; D-014).
type CreateTaskResultMeta struct {
	// ModelImmediateResponse, when non-empty, is a placeholder string handed
	// to the model so the host can return control while the task runs. It is
	// emitted under the provisional
	// `io.modelcontextprotocol/model-immediate-response` key.
	ModelImmediateResponse string
}

func (m CreateTaskResultMeta) isZero() bool {
	return m.ModelImmediateResponse == ""
}

// TasksServerCapability is the value Dockyard advertises for
// `capabilities.tasks` (brief 02 §2.6). Each bool gates an operation; the
// `requests` set is exhaustive — an absent request type means unsupported.
type TasksServerCapability struct {
	// List gates `tasks/list`.
	List bool
	// Cancel gates `tasks/cancel`.
	Cancel bool
	// ToolsCall reports that the server accepts task-augmented `tools/call`.
	ToolsCall bool
}

// CreateTaskResult is the Dockyard domain view of the schema's
// `CreateTaskResult` — the result a receiver returns for an accepted
// task-augmented request, in place of the underlying request's own result
// (brief 02 §2.3). It wraps a [Task]; the schema attaches no `resultType`
// discriminator (brief 02 §5 "avoid").
type CreateTaskResult struct {
	// Task is the durable task the request was accepted as.
	Task Task
	// Meta is the optional `_meta` carried on the result — currently just the
	// provisional model-immediate-response key. A nil map omits `_meta`.
	Meta Meta
}

// TaskID is the Dockyard domain view of the params of `tasks/get`,
// `tasks/result` and `tasks/cancel` — each takes a single `taskId` string
// (brief 02 §2.3). It is one type because the three method params are
// structurally identical.
type TaskID struct {
	// ID is the `taskId` request parameter.
	ID string
}

// ListTasksParams is the Dockyard domain view of the `tasks/list` request
// params — a `PaginatedRequest` carrying an optional opaque `cursor`
// (brief 02 §2.3).
type ListTasksParams struct {
	// Cursor is the opaque pagination cursor; empty requests the first page.
	Cursor string
}

// ListTasksResult is the Dockyard domain view of the `tasks/list` result — a
// `PaginatedResult` carrying the page of tasks and an optional `nextCursor`
// (brief 02 §2.3).
type ListTasksResult struct {
	// Tasks is the page of tasks.
	Tasks []Task
	// NextCursor is the opaque cursor for the next page; empty means the page
	// is the last one.
	NextCursor string
}

// ---- wire shapes (raw JSON; stay inside the seam) ----

// taskWire is the schema's `Task` object. `ttl` is intentionally NOT
// omitempty: the schema requires the field to be present, with `null` meaning
// unlimited.
type taskWire struct {
	TaskID        string     `json:"taskId"`
	Status        TaskStatus `json:"status"`
	StatusMessage string     `json:"statusMessage,omitempty"`
	CreatedAt     string     `json:"createdAt"`
	LastUpdatedAt string     `json:"lastUpdatedAt"`
	TTL           *int64     `json:"ttl"`
	PollInterval  *int64     `json:"pollInterval,omitempty"`
}

// taskMetadataWire is the schema's `TaskMetadata` (the request `task` field).
type taskMetadataWire struct {
	TTL *int64 `json:"ttl,omitempty"`
}

// relatedTaskWire is the schema's `RelatedTaskMetadata`, the value of the
// `io.modelcontextprotocol/related-task` `_meta` key.
type relatedTaskWire struct {
	TaskID string `json:"taskId"`
}

// tasksServerCapabilityWire is the `capabilities.tasks` block. Per schema each
// gate is an empty object when present, absent otherwise.
type tasksServerCapabilityWire struct {
	List     *struct{}               `json:"list,omitempty"`
	Cancel   *struct{}               `json:"cancel,omitempty"`
	Requests *tasksReqCapabilityWire `json:"requests,omitempty"`
}

type tasksReqCapabilityWire struct {
	Tools *tasksToolsReqWire `json:"tools,omitempty"`
}

type tasksToolsReqWire struct {
	Call *struct{} `json:"call,omitempty"`
}

// ---- method-envelope wire shapes ----

// createTaskResultWire is the schema's `CreateTaskResult` — `{ task, _meta? }`.
// It extends `Result`, whose only field is the optional `_meta`.
type createTaskResultWire struct {
	Task taskWire `json:"task"`
	Meta Meta     `json:"_meta,omitempty"`
}

// taskIDParamsWire is the params object of `tasks/get`, `tasks/result` and
// `tasks/cancel` — each is `{ "taskId": string }`.
type taskIDParamsWire struct {
	TaskID string `json:"taskId"`
}

// getTaskResultWire is the schema's `GetTaskResult` / `CancelTaskResult`:
// `Result & Task` — the flat `Task` shape, with `taskId` at the top level
// rather than nested under a `task` key.
type getTaskResultWire = taskWire

// listTasksParamsWire is the `tasks/list` request params (`PaginatedRequest`).
type listTasksParamsWire struct {
	Cursor string `json:"cursor,omitempty"`
}

// listTasksResultWire is the schema's `ListTasksResult` (`PaginatedResult`).
type listTasksResultWire struct {
	Tasks      []taskWire `json:"tasks"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// SupplyInputParams is the Dockyard-internal `dockyard/tasks/supplyInput`
// params object (Phase 25 / D-134). It is the wire half of
// [tasks.Engine.SupplyInput] — the inspector posts it to deliver an App's
// elicitation-response to a suspended `input_required` task. Distinct
// from the vendored experimental Tasks spec (which defines no standard
// shape for resuming an input_required task); the `dockyard/` method
// prefix and this struct keep it from being confused with a future
// standard wire shape.
type SupplyInputParams struct {
	// TaskID is the suspended task to resume. Required.
	TaskID string
	// Data is the opaque payload the requestor supplied — the handler
	// receives it as `tasks.InputResponse.Data` raw JSON. Absent when
	// Declined is true.
	Data json.RawMessage
	// Declined signals the requestor explicitly declined to provide
	// input. The handler receives it as `tasks.InputResponse.Declined`.
	Declined bool
}

// supplyInputParamsWire is the on-wire shape of SupplyInputParams.
type supplyInputParamsWire struct {
	TaskID   string          `json:"taskId"`
	Data     json.RawMessage `json:"data,omitempty"`
	Declined bool            `json:"declined,omitempty"`
}
