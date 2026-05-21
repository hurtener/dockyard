package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// Tasks JSON-RPC method names — the four operations the server-side receiver
// serves (vendored spec, "Task Operations"). The engine routes exactly these;
// there is deliberately no tasks/update (an overview-page artifact the
// authoritative schema does not define — brief 02 §2.4).
const (
	MethodGet    = "tasks/get"
	MethodResult = "tasks/result"
	MethodCancel = "tasks/cancel"
	MethodList   = "tasks/list"
)

// defaultPollInterval is the pollInterval (ms) the engine suggests to a
// requestor when an Options value is not set — a conservative 1s cadence.
const defaultPollInterval int64 = 1000

// RunFunc is the underlying work a task wraps. The engine runs it on a
// background goroutine; its result JSON (a CallToolResult for a tools/call
// task) becomes the task's terminal payload, fetched later via tasks/result.
//
// A RunFunc that returns a non-nil error moves the task to failed; a nil error
// moves it to completed. The handler stays sync-shaped — it returns a value
// and an error, exactly as a normal tool handler does (RFC §8.4). The richer
// TaskHandle (progress, cooperative cancellation, input_required elicitation)
// is Phase 14; Phase 13's RunFunc already receives a context the engine
// cancels on tasks/cancel, so a Phase 14 handler observes cancellation through
// the same ctx.
type RunFunc func(ctx context.Context) (json.RawMessage, error)

// Options tunes an [Engine]. The zero value is valid; a nil *Options is the
// zero value.
type Options struct {
	// Logger receives the engine's structured logs. Nil uses slog.Default().
	Logger *slog.Logger
	// GenerateID generates task identifiers. Nil uses [CryptoID] — the
	// crypto-strong default (brief 02 §4.5).
	GenerateID IDFunc
	// PollInterval is the pollInterval (ms) the engine reports on every task.
	// A value <= 0 uses defaultPollInterval. Phase 14's manifest knob feeds
	// this; Phase 13 takes it as a construction option.
	PollInterval int64
	// AdvertiseList controls whether the tasks capability advertises tasks/list
	// (and whether Dispatch serves it). The vendored spec requires a receiver
	// that cannot identify requestors NOT to advertise tasks/list — Phase 14
	// owns identifiability, so Phase 13 defaults this off and lets a caller
	// that knows it can identify requestors opt in.
	AdvertiseList bool
}

// Engine is the server-side Tasks router and lifecycle owner (RFC §8.2). It is
// a reusable artifact: one Engine is safe for concurrent use by many
// goroutines — every task created by [Engine.CreateForToolCall] runs on its
// own goroutine and concurrent [Engine.Dispatch] calls are independent.
type Engine struct {
	store  TaskStore
	codec  protocolcodec.Codec
	log    *slog.Logger
	genID  IDFunc
	pollMS int64
	listOn bool

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // taskID → cancel of its run goroutine
	waiters map[string][]chan struct{}    // taskID → terminal-status signal channels
}

// NewEngine constructs a Tasks engine over store. store must be non-nil — it is
// the persistence seam (Phase 13: [NewInMemoryStore]; Phase 14: the durable
// driver).
func NewEngine(store TaskStore, opts *Options) (*Engine, error) {
	if store == nil {
		return nil, errors.New("dockyard/runtime/tasks: NewEngine requires a non-nil TaskStore")
	}
	log := slog.Default()
	genID := CryptoID
	pollMS := defaultPollInterval
	listOn := false
	if opts != nil {
		if opts.Logger != nil {
			log = opts.Logger
		}
		if opts.GenerateID != nil {
			genID = opts.GenerateID
		}
		if opts.PollInterval > 0 {
			pollMS = opts.PollInterval
		}
		listOn = opts.AdvertiseList
	}
	return &Engine{
		store:   store,
		codec:   protocolcodec.CodecFor(protocolcodec.DefaultVersion),
		log:     log,
		genID:   genID,
		pollMS:  pollMS,
		listOn:  listOn,
		cancels: make(map[string]context.CancelFunc),
		waiters: make(map[string][]chan struct{}),
	}, nil
}

// CreateToolCallParams names a task-augmented tools/call the engine should
// accept as a task. It is the runtime-facing input to [Engine.CreateForToolCall];
// raw experimental protocol structs never appear here (P3).
type CreateToolCallParams struct {
	// ToolName is the tool being called.
	ToolName string
	// TaskMeta is the requestor's task-augmentation metadata (the `task` field
	// of the request params) — currently just the requested TTL.
	TaskMeta protocolcodec.TaskMeta
	// AuthContext is an opaque requestor-identity token. Phase 13 records it on
	// the task; Phase 14 binds access to it. Empty means unauthenticated.
	AuthContext string
	// Run is the underlying tool work. Required.
	Run RunFunc
}

// CreateForToolCall accepts a task-augmented tools/call: it generates a task
// ID, durably records the task in the working status, starts the underlying
// work on a background goroutine, and returns the CreateTaskResult JSON the
// receiver sends back in place of the immediate CallToolResult (vendored spec,
// "Creating Tasks"; RFC §8.3).
//
// The returned JSON is a fully-encoded CreateTaskResult, produced through
// internal/protocolcodec. The actual tool result is fetched later through
// tasks/result once the task reaches a terminal status.
func (e *Engine) CreateForToolCall(ctx context.Context, p CreateToolCallParams) (json.RawMessage, error) {
	if p.Run == nil {
		return nil, fmt.Errorf("%w: CreateForToolCall requires a non-nil Run", ErrInvalidParams)
	}
	id, err := e.genID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	poll := e.pollMS
	rec := TaskRecord{
		ID:           id,
		Status:       protocolcodec.TaskWorking,
		CreatedAt:    now,
		UpdatedAt:    now,
		RequestedTTL: p.TaskMeta.TTL,
		PollInterval: &poll,
		Method:       "tools/call",
		ToolName:     p.ToolName,
		AuthContext:  p.AuthContext,
	}
	if err := e.store.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("dockyard/runtime/tasks: create task: %w", err)
	}

	// The run goroutine is bound to a context the engine cancels on
	// tasks/cancel — cancellation is cooperative (the handler observes ctx).
	// The task is detached from the request context: a tools/call returns its
	// CreateTaskResult immediately, so the task must outlive the request.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	e.mu.Lock()
	e.cancels[id] = cancel
	e.mu.Unlock()

	// runTask owns runCtx for its whole lifetime and always releases cancel on
	// exit; tasks/cancel may also call it early. Calling a CancelFunc twice is
	// safe, so both paths are correct.
	go e.runTask(runCtx, cancel, id, p.Run)

	e.log.InfoContext(ctx, "task created",
		slog.String("taskId", id), slog.String("tool", p.ToolName))

	return e.codec.EncodeCreateTaskResult(protocolcodec.CreateTaskResult{Task: rec.Task()})
}

// runTask executes a task's RunFunc and records the terminal outcome. It is the
// single place a task leaves the working status by completion or failure;
// tasks/cancel is the only other terminal path. runTask always calls cancel on
// exit, releasing the run context's resources regardless of how the task ends.
func (e *Engine) runTask(ctx context.Context, cancel context.CancelFunc, id string, run RunFunc) {
	defer cancel()
	defer func() {
		// A panic in app handler code must never crash the engine goroutine —
		// "never panic across the MCP boundary" (AGENTS.md §5, §13).
		if r := recover(); r != nil {
			e.log.ErrorContext(ctx, "task handler panicked",
				slog.String("taskId", id), slog.Any("panic", r))
			e.finish(ctx, id, protocolcodec.TaskFailed,
				fmt.Sprintf("tool handler panicked: %v", r),
				TaskResult{Err: fmt.Sprintf("tool handler panicked: %v", r)})
		}
	}()

	payload, err := run(ctx)
	if err != nil {
		e.finish(ctx, id, protocolcodec.TaskFailed, err.Error(), TaskResult{Err: err.Error()})
		return
	}
	e.finish(ctx, id, protocolcodec.TaskCompleted, "", TaskResult{Payload: payload})
}

// finish records a task's terminal outcome and wakes any tasks/result waiters.
// A finish onto an already-cancelled task is a no-op transition (the store
// treats a same-status write as success) but the result is still recorded so a
// late tasks/result has something to return — cancellation is cooperative.
func (e *Engine) finish(
	ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, res TaskResult,
) {
	if _, err := e.store.Transition(ctx, id, status, msg); err != nil {
		// A cancelled task rejects no further transition because the store
		// treats same-status as a no-op; any other error is logged, not
		// panicked.
		if !errors.Is(err, ErrIllegalTransition) {
			e.log.ErrorContext(ctx, "task finish transition failed",
				slog.String("taskId", id), slog.String("error", err.Error()))
		}
	}
	if err := e.store.SetResult(ctx, id, res); err != nil {
		e.log.ErrorContext(ctx, "task set result failed",
			slog.String("taskId", id), slog.String("error", err.Error()))
	}
	e.wake(id)
}

// wake signals every tasks/result goroutine waiting on id that the task may
// have reached a terminal status, and drops the cancel func.
func (e *Engine) wake(id string) {
	e.mu.Lock()
	chans := e.waiters[id]
	delete(e.waiters, id)
	delete(e.cancels, id)
	e.mu.Unlock()
	for _, ch := range chans {
		close(ch)
	}
}

// waitChan registers and returns a channel closed when the task reaches a
// terminal status. The caller must always drain or abandon it; wake closes it.
func (e *Engine) waitChan(id string) chan struct{} {
	ch := make(chan struct{})
	e.mu.Lock()
	e.waiters[id] = append(e.waiters[id], ch)
	e.mu.Unlock()
	return ch
}

// Capability returns the protocolcodec.TasksServerCapability the engine
// advertises — capability-driven, never a host matrix (AGENTS.md §6). It
// always advertises cancel and task-augmented tools/call; tasks/list is
// advertised only when AdvertiseList was set, honouring the vendored spec's
// rule that a receiver unable to identify requestors must not advertise it.
func (e *Engine) Capability() protocolcodec.TasksServerCapability {
	return protocolcodec.TasksServerCapability{
		List:      e.listOn,
		Cancel:    true,
		ToolsCall: true,
	}
}
