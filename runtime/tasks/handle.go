package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
)

// This file is the TaskHandle handler API (RFC §8.4). A tool handler doing
// genuinely long work receives a TaskHandle: it stays sync-shaped — it returns
// a value and an error exactly as a normal handler does — and uses the handle
// to report progress, post status messages, observe cooperative cancellation,
// and request input mid-task via input_required elicitation.
//
// TaskHandle exposes only clean Dockyard types. No raw experimental protocol
// struct (a protocolcodec wire type) ever reaches it — P3 / brief 02 §5. The
// progress fraction is a float64, the input prompt and response are plain
// Dockyard structs; the protocol encoding stays inside internal/protocolcodec.

// TaskHandle is the handler-facing API for a long-running task. It is passed to
// a HandleFunc; the handler must not retain it past the call. A TaskHandle is
// safe for concurrent use by the one handler goroutine and the engine.
type TaskHandle interface {
	// Progress records the task's completion fraction (0.0–1.0) and an optional
	// human-readable message. It is advisory — a requestor learns it by polling
	// tasks/get (the StatusMessage) — and best-effort: a Progress call on a task
	// that has already left the working status returns an error rather than
	// forcing an illegal transition.
	Progress(ctx context.Context, fraction float64, message string) error

	// Status sets the task's human-readable status message without changing the
	// completion fraction — for a phase change a fraction cannot express.
	Status(ctx context.Context, message string) error

	// Cancelled reports whether the task has been cooperatively cancelled
	// (tasks/cancel was called). A long handler polls Cancelled at safe points
	// and unwinds cleanly when it is true — cancellation is cooperative, never a
	// forced kill (brief 02 §4.7). The handler's context is also cancelled, so
	// a handler may instead select on ctx.Done().
	Cancelled() bool

	// RequireInput drives an input_required elicitation: it transitions the task
	// to input_required, blocks until the requestor supplies input (delivered
	// over the tasks/result channel) or the task is cancelled, then transitions
	// back to working and returns the response. It is how a sync-shaped handler
	// pauses mid-task for input (RFC §8.4). RequireInput returns an error if the
	// task is cancelled while waiting.
	RequireInput(ctx context.Context, prompt InputPrompt) (InputResponse, error)

	// RequestInput performs the modern Tasks mid-flight lifecycle. The request
	// is persisted before the task blocks and the matching tasks/update response
	// is persisted before this method resumes.
	RequestInput(ctx context.Context, req InputRequest) error

	// ModernInputResponse returns a durably accepted response by request key.
	ModernInputResponse(ctx context.Context, key string) (TaskInputResponse, bool, error)
}

func (h *taskHandle) RequestInput(ctx context.Context, req InputRequest) error {
	_, err := h.engine.RequestInput(ctx, h.id, req)
	return err
}

func (h *taskHandle) ModernInputResponse(ctx context.Context, key string) (TaskInputResponse, bool, error) {
	rec, err := h.engine.store.Get(ctx, h.id)
	if err != nil {
		return TaskInputResponse{}, false, err
	}
	resp, ok := rec.InputResponses[key]
	return resp, ok, nil
}

// InputPrompt is a request for input mid-task — a clean Dockyard type, never a
// raw elicitation protocol struct (P3). It carries a human-readable prompt and
// an opaque schema hint the App UI / host renders into a form.
type InputPrompt struct {
	// Message is the human-readable prompt shown to the requestor.
	Message string
	// Schema is an optional opaque JSON Schema describing the expected input
	// shape; an empty value asks for free-form input. It is contract data, not a
	// protocol envelope. The engine records it verbatim with the outstanding
	// elicitation and exposes it through Engine.PendingInput for a host to read
	// and render a form. NOTE (v1.5): no V1 wire/transport surface yet pushes the
	// schema to the requestor — only Message reaches a poller (as the task
	// StatusMessage). Surfacing Schema to the App/inspector is a tracked
	// follow-up (docs/V2-BACKLOG.md); until then a handler should not rely on the
	// requestor receiving the schema. Message must carry enough for a free-form
	// reply.
	Schema []byte
}

// InputResponse is the requestor's reply to an InputPrompt — the raw input JSON
// the handler interprets against its own contract.
type InputResponse struct {
	// Data is the requestor-supplied input as raw JSON.
	Data []byte
	// Declined is true when the requestor explicitly declined to provide input
	// rather than supplying it; the handler decides how to proceed.
	Declined bool
}

// HandleFunc is the long-running-task handler shape (RFC §8.4). It is the
// TaskHandle-bearing counterpart of RunFunc: a handler that needs progress,
// status, cooperative cancellation or input_required elicitation takes a
// HandleFunc; a handler that needs none keeps the simpler RunFunc. Both stay
// sync-shaped — they return a value and an error.
type HandleFunc func(ctx context.Context, h TaskHandle) (json.RawMessage, error)

// asRunFunc adapts a HandleFunc into a RunFunc bound to a concrete handle, so
// the engine's single run path (runTask) serves both handler shapes.
func (e *Engine) asRunFunc(id string, fn HandleFunc) RunFunc {
	return func(ctx context.Context) (json.RawMessage, error) {
		h := &taskHandle{engine: e, id: id, ctx: ctx}
		return fn(ctx, h)
	}
}

// taskHandle is the concrete TaskHandle the engine hands a HandleFunc. It is a
// thin client of the engine's store and waiter machinery — it owns no state
// beyond the task ID and the input-elicitation rendezvous.
type taskHandle struct {
	engine *Engine
	id     string
	ctx    context.Context

	mu      sync.Mutex
	inputCh chan InputResponse // set while an elicitation is outstanding
}

// Progress records a completion fraction and message as the task's status
// message. The fraction is clamped to [0,1]; the engine keeps the task in
// working — Progress never transitions the lifecycle. On a successful update
// it emits a mid-flight obs/v1 task.progress event (PhaseProgress) carrying
// the clamped fraction and the raw message, so the bridge's View-side
// task-progress channel can render a live percentage (RFC §8.4, D-171). A
// Progress call on a task that has left working returns an error and emits
// nothing.
func (h *taskHandle) Progress(ctx context.Context, fraction float64, message string) error {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	msg := fmt.Sprintf("%.0f%%", fraction*100)
	if message != "" {
		msg = fmt.Sprintf("%s — %s", msg, message)
	}
	if err := h.setStatusMessage(ctx, msg); err != nil {
		return err
	}
	frac := fraction
	h.emitProgress(ctx, message, &frac)
	return nil
}

// Status sets the task's status message verbatim. On a successful update it
// emits an obs/v1 task.progress event (PhaseProgress) with the message and no
// fraction — a phase change a fraction cannot express (RFC §8.4, D-171).
func (h *taskHandle) Status(ctx context.Context, message string) error {
	if err := h.setStatusMessage(ctx, message); err != nil {
		return err
	}
	h.emitProgress(ctx, message, nil)
	return nil
}

// emitProgress emits a mid-flight obs/v1 task.progress event (PhaseProgress)
// for the task. It is fire-and-forget — the emit path is non-blocking (P2), so
// it never blocks the handler — and rides a child of the task's lifecycle span
// so the progress point correlates with the start/end events (RFC §11.2). A
// nil fraction is a status-only point.
func (h *taskHandle) emitProgress(ctx context.Context, message string, fraction *float64) {
	h.engine.rec.TaskEvent(ctx, h.engine.taskSpan(h.id).Child(), obs.PhaseProgress, obs.TaskProgressPayload{
		TaskID:   h.id,
		Status:   string(protocolcodec.TaskWorking),
		Message:  message,
		Fraction: fraction,
	}, nil)
}

// setStatusMessage writes a status message by transitioning working→working —
// a same-status transition the store treats as a metadata-only update (it
// refreshes StatusMessage without a lifecycle move). A task no longer in
// working (cancelled, or moved to input_required) rejects the update with a
// typed error rather than forcing an illegal transition.
func (h *taskHandle) setStatusMessage(ctx context.Context, message string) error {
	rec, err := h.engine.store.Get(ctx, h.id)
	if err != nil {
		return err
	}
	if rec.Status != protocolcodec.TaskWorking {
		return fmt.Errorf("%w: cannot report progress on task %q in status %q",
			ErrIllegalTransition, h.id, rec.Status)
	}
	if _, err := h.engine.store.Transition(ctx, h.id, protocolcodec.TaskWorking, message); err != nil {
		return err
	}
	return nil
}

// Cancelled reports cooperative cancellation by reading the task's status.
func (h *taskHandle) Cancelled() bool {
	if h.ctx != nil && h.ctx.Err() != nil {
		return true
	}
	rec, err := h.engine.store.Get(context.Background(), h.id)
	if err != nil {
		return false
	}
	return rec.Status == protocolcodec.TaskCancelled
}

// RequireInput drives the input_required round-trip. The engine's elicitation
// delivery path (the transport mount, or a test driver) calls
// Engine.SupplyInput to hand the response back; RequireInput blocks on the
// rendezvous channel until that happens or the task is cancelled.
func (h *taskHandle) RequireInput(ctx context.Context, prompt InputPrompt) (InputResponse, error) {
	ch := make(chan InputResponse, 1)
	h.mu.Lock()
	h.inputCh = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.inputCh = nil
		h.mu.Unlock()
	}()

	// Register the outstanding elicitation on the engine and move the task to
	// input_required so a poller sees the task is waiting.
	if err := h.engine.beginElicitation(ctx, h.id, prompt, h); err != nil {
		return InputResponse{}, err
	}

	select {
	case resp := <-ch:
		// Input supplied — return the task to working and hand the reply back.
		if _, err := h.engine.store.Transition(ctx, h.id, protocolcodec.TaskWorking,
			"input received, resuming"); err != nil {
			return InputResponse{}, err
		}
		h.engine.endElicitation(h.id)
		return resp, nil
	case <-ctx.Done():
		h.engine.endElicitation(h.id)
		return InputResponse{}, fmt.Errorf("%w: input_required wait cancelled: %w",
			ErrInvalidParams, ctx.Err())
	}
}

// deliver hands an input response to a blocked RequireInput. It returns false
// when no elicitation is outstanding on this handle.
func (h *taskHandle) deliver(resp InputResponse) bool {
	h.mu.Lock()
	ch := h.inputCh
	h.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}
