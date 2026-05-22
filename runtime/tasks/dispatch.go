package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
)

// Dispatch routes one tasks/* JSON-RPC request and returns its result JSON.
//
// It is the transport-agnostic seam: the go-sdk cannot route a method outside
// its fixed dispatch table (brief 03), so Dockyard routes tasks/* itself.
// Phase 14 mounts Dispatch ahead of the SDK server on the live transport; the
// inspector and integration tests drive it directly. method is the JSON-RPC
// method name; params is the raw request params object.
//
// A non-nil error is a typed Tasks error — map it to a JSON-RPC error code
// with [JSONRPCCode]. Dispatch never panics across the boundary.
func (e *Engine) Dispatch(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case MethodGet:
		return e.handleGet(ctx, params)
	case MethodResult:
		return e.handleResult(ctx, params)
	case MethodCancel:
		return e.handleCancel(ctx, params)
	case MethodList:
		if !e.listOn {
			// tasks/list is not advertised, so it is not served — the vendored
			// spec gates the operation on the tasks.list capability.
			return nil, fmt.Errorf("%w: %q (tasks/list not advertised)", ErrUnknownMethod, method)
		}
		return e.handleList(ctx, params)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownMethod, method)
	}
}

// handleGet serves tasks/get — a non-blocking poll returning the current task
// state (vendored spec, "Getting Tasks").
func (e *Engine) handleGet(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	p, err := e.codec.DecodeTaskIDParams(params)
	if err != nil {
		return nil, err
	}
	rec, err := e.store.Get(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return e.codec.EncodeGetTaskResult(rec.Task())
}

// handleResult serves tasks/result — it BLOCKS until the task reaches a
// terminal status, then returns exactly what the underlying request would have
// returned (vendored spec, "Retrieving Task Results", "Result Retrieval"
// rules 2–4). For a failed task it returns a JSON-RPC error.
func (e *Engine) handleResult(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	p, err := e.codec.DecodeTaskIDParams(params)
	if err != nil {
		return nil, err
	}
	rec, err := e.store.Get(ctx, p.ID)
	if err != nil {
		return nil, err
	}

	// Block until terminal. Register a waiter BEFORE re-checking the status so
	// a task that finishes between the first Get and the registration cannot
	// be missed (the engine closes the waiter channel on every finish).
	for !rec.Status.IsTerminal() {
		ch := e.waitChan(p.ID)
		// Re-read: the task may have finished between Get and waitChan.
		rec, err = e.store.Get(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if rec.Status.IsTerminal() {
			break
		}
		select {
		case <-ch:
			rec, err = e.store.Get(ctx, p.ID)
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			// The requestor disconnected or cancelled the RPC; this is not a
			// task error — the task keeps running and may be polled again.
			return nil, fmt.Errorf("%w: tasks/result wait cancelled: %w", ErrInvalidParams, ctx.Err())
		}
	}

	// Terminal. A failed task surfaces the underlying request's error; the
	// vendored spec requires tasks/result to return exactly that.
	if rec.Result.Err != "" {
		return nil, fmt.Errorf("%w: task %q failed: %s", ErrInvalidParams, p.ID, rec.Result.Err)
	}
	// The related-task association key MUST be stamped on a tasks/result
	// response — the result structure itself does not carry the task ID
	// (vendored spec, "Associating Task-Related Messages"). The payload is the
	// underlying CallToolResult; the codec merges the key into its _meta.
	return e.stampRelatedTask(rec.Result.Payload, p.ID)
}

// handleCancel serves tasks/cancel — it transitions the task to cancelled
// BEFORE responding (vendored spec, "Cancelling Tasks" rule 2) and rejects a
// cancel of an already-terminal task with -32602 (rule 1).
func (e *Engine) handleCancel(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	p, err := e.codec.DecodeTaskIDParams(params)
	if err != nil {
		return nil, err
	}
	rec, err := e.store.Get(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if rec.Status.IsTerminal() {
		return nil, fmt.Errorf("%w: cannot cancel task %q already in terminal status %q",
			ErrAlreadyTerminal, p.ID, rec.Status)
	}

	// Record the cancelled outcome and flip the status to `cancelled` BEFORE
	// signalling the handler's context. Cancelling the run context first would
	// race the handler's cooperative unwind: a handler that observes ctx.Done()
	// promptly can return and drive finish() to `failed` before this transition
	// runs, leaving tasks/cancel with an illegal `failed → cancelled` move (the
	// race the Wave 5 checkpoint surfaced — D-072). Transitioning first makes
	// the store terminal-`cancelled` immediately; the handler's later finish()
	// then sees an already-terminal task and is a cooperative no-op.
	//
	// The result is written before the transition so a tasks/result waiter,
	// which unblocks the instant it sees a terminal status, always finds the
	// payload already present.
	_ = e.store.SetResult(ctx, p.ID, TaskResult{Err: "task cancelled"})
	rec, err = e.store.Transition(ctx, p.ID, protocolcodec.TaskCancelled,
		"The task was cancelled by request.")
	if err != nil {
		return nil, err
	}

	// Now signal the running handler's context (cooperative cancellation) so a
	// handler that checks ctx promptly stops and unwinds.
	e.mu.Lock()
	cancel := e.cancels[p.ID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Emit the obs/v1 task.progress terminal event for the cancellation (P2) —
	// tasks/cancel is a terminal path the run goroutine's finish does not cover.
	e.rec.TaskEvent(ctx, e.taskSpan(p.ID).Child(), obs.PhaseEnd, obs.TaskProgressPayload{
		TaskID:  p.ID,
		Status:  string(protocolcodec.TaskCancelled),
		Message: "The task was cancelled by request.",
	}, nil)

	e.wake(p.ID)

	return e.codec.EncodeGetTaskResult(rec.Task())
}

// handleList serves tasks/list — a cursor-paginated listing (vendored spec,
// "Listing Tasks"). It is reached only when tasks/list is advertised. This
// unscoped listing is used by the inspector and the unauthenticated path;
// DispatchAs serves the auth-scoped listing for an identified requestor.
func (e *Engine) handleList(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	p, err := e.codec.DecodeListTasksParams(params)
	if err != nil {
		return nil, err
	}
	recs, next, err := e.store.List(ctx, p.Cursor, 0)
	if err != nil {
		return nil, err
	}
	return e.encodeList(recs, next)
}

// encodeList projects a page of records into a ListTasksResult and encodes it
// through the codec — the single encode path shared by the unscoped handleList
// and the auth-scoped handleListScoped.
func (e *Engine) encodeList(recs []TaskRecord, next string) (json.RawMessage, error) {
	out := protocolcodec.ListTasksResult{NextCursor: next}
	out.Tasks = make([]protocolcodec.Task, 0, len(recs))
	for _, r := range recs {
		out.Tasks = append(out.Tasks, r.Task())
	}
	return e.codec.EncodeListTasksResult(out)
}

// stampRelatedTask merges the related-task association _meta key (taskId) into
// the underlying result's _meta, through the codec — the key literal lives
// only inside internal/protocolcodec (P3). The payload is a CallToolResult
// JSON object; an empty payload yields a bare result carrying only the key.
func (e *Engine) stampRelatedTask(payload json.RawMessage, taskID string) (json.RawMessage, error) {
	var obj map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &obj); err != nil {
			return nil, fmt.Errorf("%w: task result payload is not a JSON object: %w",
				ErrInvalidParams, err)
		}
	}
	if obj == nil {
		obj = map[string]any{}
	}
	var base protocolcodec.Meta
	if existing, ok := obj["_meta"].(map[string]any); ok {
		base = protocolcodec.Meta(existing)
	}
	merged, err := e.codec.EncodeRelatedTaskMeta(base, taskID)
	if err != nil {
		return nil, err
	}
	obj["_meta"] = map[string]any(merged)
	return json.Marshal(obj)
}
