package tasks

import (
	"errors"
	"fmt"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// Sentinel errors for the Tasks engine. Every one maps to a JSON-RPC error
// code via [JSONRPCCode]; surfacing a typed error rather than panicking is the
// "never panic across the MCP boundary" rule made concrete (AGENTS.md §5, §13).
var (
	// ErrTaskNotFound is returned when a taskId does not name a known task —
	// the spec's "Task not found" case. JSON-RPC -32602 (Invalid params).
	ErrTaskNotFound = errors.New("dockyard/runtime/tasks: task not found")

	// ErrIllegalTransition is returned when a lifecycle transition is not one
	// of the spec-legal paths (RFC §8.3). JSON-RPC -32603 (Internal error):
	// an illegal transition is a server-side bug, not a bad request.
	ErrIllegalTransition = errors.New("dockyard/runtime/tasks: illegal task status transition")

	// ErrAlreadyTerminal is returned when tasks/cancel targets a task already
	// in a terminal status — the spec mandates -32602 (Invalid params) here.
	ErrAlreadyTerminal = errors.New("dockyard/runtime/tasks: task already in a terminal status")

	// ErrUnknownMethod is returned by Dispatch for a method outside the tasks/*
	// set it routes. JSON-RPC -32601 (Method not found).
	ErrUnknownMethod = errors.New("dockyard/runtime/tasks: unknown tasks method")

	// ErrInvalidParams is returned when a request's params are malformed.
	// JSON-RPC -32602 (Invalid params).
	ErrInvalidParams = errors.New("dockyard/runtime/tasks: invalid params")

	// ErrConcurrencyCap is returned by task creation when the requestor is at
	// the per-requestor concurrent-task cap (RFC §8.5; brief 02 §4.6). JSON-RPC
	// -32602 (Invalid params): the requestor must retire a task before creating
	// another — a request the receiver cannot currently honour.
	ErrConcurrencyCap = errors.New("dockyard/runtime/tasks: per-requestor task concurrency cap reached")

	// ErrNoPendingInput is returned by [Engine.SupplyInput] when the named task
	// has no outstanding input_required elicitation. JSON-RPC -32602.
	ErrNoPendingInput = errors.New("dockyard/runtime/tasks: task has no pending input_required elicitation")

	// ErrCrossContext is returned when a tasks/get|result|cancel names a task
	// that exists but belongs to a different authorization context — the
	// auth-context binding rejection (RFC §8.5; brief 02 §4.5). It is
	// deliberately indistinguishable to the caller from ErrTaskNotFound at the
	// JSON-RPC layer (both map to -32602): the receiver must not leak the
	// existence of another context's task. JSON-RPC -32602 (Invalid params).
	ErrCrossContext = errors.New("dockyard/runtime/tasks: task not found")
)

// JSON-RPC error codes used by the Tasks engine, per the vendored spec's
// "Error Handling" section (mcp-tasks-experimental.mdx).
const (
	// CodeMethodNotFound is JSON-RPC -32601.
	CodeMethodNotFound = -32601
	// CodeInvalidParams is JSON-RPC -32602.
	CodeInvalidParams = -32602
	// CodeInternalError is JSON-RPC -32603.
	CodeInternalError = -32603
)

// JSONRPCCode maps a Tasks engine error to the JSON-RPC error code the
// receiver must return for it (vendored spec, "Protocol Errors"). An error not
// recognised here maps to -32603 (Internal error), the spec's catch-all.
func JSONRPCCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrUnknownMethod):
		return CodeMethodNotFound
	case errors.Is(err, ErrTaskNotFound),
		errors.Is(err, ErrAlreadyTerminal),
		errors.Is(err, ErrInvalidParams),
		errors.Is(err, ErrConcurrencyCap),
		errors.Is(err, ErrNoPendingInput),
		errors.Is(err, ErrCrossContext),
		errors.Is(err, protocolcodec.ErrMalformedMeta):
		return CodeInvalidParams
	default:
		return CodeInternalError
	}
}

// transitionError builds an ErrIllegalTransition wrapping the from/to statuses.
func transitionError(from, to protocolcodec.TaskStatus) error {
	return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
}
