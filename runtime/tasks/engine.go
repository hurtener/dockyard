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
	"github.com/hurtener/dockyard/runtime/obs"
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
	// owns identifiability, so this defaults off.
	//
	// AdvertiseList alone is not sufficient: tasks/list is served only when the
	// engine can also identify requestors (RequestorIdentifiable). A receiver
	// that opts AdvertiseList on but leaves RequestorIdentifiable off — the
	// unauthenticated single-user stdio case — still does not advertise or
	// serve tasks/list (brief 02 §4.5).
	AdvertiseList bool
	// RequestorIdentifiable declares that the deployment can identify the
	// authorization context of each requestor — true for an authenticated HTTP
	// deployment, false for unauthenticated single-user stdio. It gates both the
	// tasks/list advertisement and auth-context binding: when false the engine
	// withholds tasks/list entirely (RFC §8.5; brief 02 §4.5 "Avoid").
	RequestorIdentifiable bool
	// Lifecycle holds the manifest-tunable task-lifecycle limits — max TTL,
	// default TTL, per-requestor concurrency cap, purge interval (RFC §8.5).
	// The zero value disables every limit.
	Lifecycle Lifecycle

	// Obs is the obs/v1 observability emitter the engine emits task lifecycle
	// events to (RFC §11.2, P2). A nil emitter disables emission; the engine is
	// headless either way. The runtime EMITS the obs/v1 task.progress stream;
	// the inspector consumes it — nothing reads engine internals to observe
	// (CLAUDE.md §6).
	Obs obs.Emitter

	// ServerID is the stable server identity stamped onto the engine's emitted
	// obs/v1 events. When empty it defaults to "dockyard-tasks".
	ServerID string
}

// Engine is the server-side Tasks router and lifecycle owner (RFC §8.2). It is
// a reusable artifact: one Engine is safe for concurrent use by many
// goroutines — every task created by [Engine.CreateForToolCall] runs on its
// own goroutine and concurrent [Engine.Dispatch] calls are independent.
type Engine struct {
	store       TaskStore
	codec       protocolcodec.Codec
	log         *slog.Logger
	rec         *obs.Recorder // obs/v1 emit helper; never nil
	genID       IDFunc
	pollMS      int64
	listOn      bool
	identifiabl bool
	life        Lifecycle

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // taskID → cancel of its run goroutine
	waiters map[string][]chan struct{}    // taskID → terminal-status signal channels
	elicits map[string]*elicitation       // taskID → outstanding input_required prompt
	spans   map[string]obs.SpanContext    // taskID → obs/v1 trace span correlating its events

	sweep *purgeSweep // background TTL purge sweep; nil when no interval set
}

// elicitation is one outstanding input_required round-trip — the prompt the
// handler raised and the live taskHandle waiting on the reply.
type elicitation struct {
	prompt InputPrompt
	handle *taskHandle
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
	identifiable := false
	var life Lifecycle
	var emitter obs.Emitter
	serverID := "dockyard-tasks"
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
		identifiable = opts.RequestorIdentifiable
		life = opts.Lifecycle
		emitter = opts.Obs
		if opts.ServerID != "" {
			serverID = opts.ServerID
		}
	}
	// tasks/list is served only when it is BOTH opted-in AND the deployment can
	// identify requestors — a receiver that cannot identify requestors must not
	// advertise tasks/list (RFC §8.5; brief 02 §4.5).
	e := &Engine{
		store:       store,
		codec:       protocolcodec.CodecFor(protocolcodec.DefaultVersion),
		log:         log,
		rec:         obs.NewRecorder(emitter, serverID),
		genID:       genID,
		pollMS:      pollMS,
		listOn:      listOn && identifiable,
		identifiabl: identifiable,
		life:        life,
		cancels:     make(map[string]context.CancelFunc),
		waiters:     make(map[string][]chan struct{}),
		elicits:     make(map[string]*elicitation),
		spans:       make(map[string]obs.SpanContext),
	}
	e.sweep = newPurgeSweep(store, life.PurgeInterval, log)
	return e, nil
}

// StartSweep launches the background TTL purge sweep bound to ctx, if a purge
// interval was configured (RFC §8.5). It is idempotent; the sweep stops when
// ctx is cancelled or [Engine.StopSweep] is called. A no-op when no interval
// was set — the in-memory single-user case.
func (e *Engine) StartSweep(ctx context.Context) {
	e.sweep.Start(ctx)
}

// StopSweep cancels the background TTL purge sweep and blocks until its
// goroutine has exited. It is idempotent and safe even if StartSweep was never
// called — the clean-shutdown half of the reusable-artifact contract.
func (e *Engine) StopSweep() {
	e.sweep.Stop()
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
	// AuthContext is an opaque requestor-identity token. The engine records it
	// on the task, binds tasks/get|result|cancel to it, and scopes tasks/list
	// to it (RFC §8.5). Empty means an unauthenticated requestor.
	AuthContext string
	// Run is the underlying tool work, the simple sync-shaped handler shape.
	// Exactly one of Run or Handle must be set.
	Run RunFunc
	// Handle is the TaskHandle-bearing handler shape — for a handler that needs
	// progress, status, cooperative cancellation or input_required elicitation
	// (RFC §8.4). Exactly one of Run or Handle must be set.
	Handle HandleFunc
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
	if (p.Run == nil) == (p.Handle == nil) {
		return nil, fmt.Errorf("%w: CreateForToolCall requires exactly one of Run or Handle", ErrInvalidParams)
	}

	// Enforce the per-requestor concurrent-task cap before anything durable is
	// written — the brief 02 §4.6 resource-exhaustion guard (RFC §8.5).
	if err := e.checkConcurrencyCap(ctx, p.AuthContext); err != nil {
		return nil, err
	}

	id, err := e.genID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	poll := e.pollMS
	// Clamp the requested TTL to the manifest max and apply the default; the
	// enforced TTL fixes the task's expiry, which the purge sweep reaps on.
	ttl := e.life.enforcedTTL(p.TaskMeta.TTL)
	rec := TaskRecord{
		ID:           id,
		Status:       protocolcodec.TaskWorking,
		CreatedAt:    now,
		UpdatedAt:    now,
		RequestedTTL: p.TaskMeta.TTL,
		TTL:          ttl,
		PollInterval: &poll,
		Method:       "tools/call",
		ToolName:     p.ToolName,
		AuthContext:  p.AuthContext,
	}
	if ttl != nil {
		rec.ExpiresAt = now.Add(time.Duration(*ttl) * time.Millisecond)
	}
	if err := e.store.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("dockyard/runtime/tasks: create task: %w", err)
	}

	// Resolve the handler shape: a HandleFunc is adapted into a RunFunc bound to
	// the task's TaskHandle, so the engine's single run path serves both shapes.
	run := p.Run
	if run == nil {
		run = e.asRunFunc(id, p.Handle)
	}

	// The run goroutine is bound to a context the engine cancels on
	// tasks/cancel — cancellation is cooperative (the handler observes ctx).
	// The task is detached from the request context: a tools/call returns its
	// CreateTaskResult immediately, so the task must outlive the request.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	// One obs/v1 trace span correlates the whole task lifecycle: the start
	// event here, any progress events, and the terminal event in finish all
	// share this span (RFC §11.2 — W3C Trace Context correlation).
	span := obs.NewTrace()
	e.mu.Lock()
	e.cancels[id] = cancel
	e.spans[id] = span
	e.mu.Unlock()

	// Emit the obs/v1 task.progress start event (P2) — a task was created.
	e.rec.TaskEvent(ctx, span, obs.PhaseStart, obs.TaskProgressPayload{
		TaskID: id,
		Status: string(protocolcodec.TaskWorking),
		Tool:   p.ToolName,
	}, nil)

	// runTask owns runCtx for its whole lifetime and always releases cancel on
	// exit; tasks/cancel may also call it early. Calling a CancelFunc twice is
	// safe, so both paths are correct.
	go e.runTask(runCtx, cancel, id, run)

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
//
// A finish onto a task that is already terminal is a cooperative no-op: it
// neither transitions nor overwrites the recorded result. This is the
// cooperative-cancellation contract (brief 02 §4.7) — tasks/cancel transitions
// the task to `cancelled` and records the cancelled result before signalling
// the handler's context, so when the handler then unwinds and calls finish the
// authoritative cancelled outcome must be preserved, not clobbered by the
// handler's unwind error (the race the Wave 5 checkpoint surfaced — D-072).
//
// For a task still running, the result payload is recorded BEFORE the
// terminal-status transition: a tasks/result waiter unblocks the instant it
// observes a terminal status, so writing SetResult first guarantees the
// payload is already present whenever the status is terminal. Writing the
// transition first would open a window in which tasks/result sees `completed`
// but reads an empty payload.
func (e *Engine) finish(
	ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, res TaskResult,
) {
	// If the task is already terminal — tasks/cancel won the race — preserve its
	// recorded outcome and only ensure waiters are woken.
	if rec, err := e.store.Get(ctx, id); err == nil && rec.Status.IsTerminal() {
		e.wake(id)
		return
	}
	if err := e.store.SetResult(ctx, id, res); err != nil {
		e.log.ErrorContext(ctx, "task set result failed",
			slog.String("taskId", id), slog.String("error", err.Error()))
	}
	if _, err := e.store.Transition(ctx, id, status, msg); err != nil {
		// A task that became terminal between the Get above and here (a
		// concurrent tasks/cancel) rejects this transition; that is the
		// cooperative no-op, logged at debug, never panicked.
		if !errors.Is(err, ErrIllegalTransition) {
			e.log.ErrorContext(ctx, "task finish transition failed",
				slog.String("taskId", id), slog.String("error", err.Error()))
		}
	}
	// Emit the obs/v1 task.progress terminal event (P2). A child span of the
	// task's lifecycle span keeps the end correlated with the start.
	var termErr error
	if status == protocolcodec.TaskFailed {
		termErr = errors.New(res.Err)
	}
	e.rec.TaskEvent(ctx, e.taskSpan(id).Child(), obs.PhaseEnd, obs.TaskProgressPayload{
		TaskID:  id,
		Status:  string(status),
		Message: msg,
	}, termErr)
	e.wake(id)
}

// taskSpan returns the obs/v1 lifecycle span recorded for id, or a fresh trace
// if none is held. It does not drop the entry — wake clears engine maps for the
// task once it is terminal.
func (e *Engine) taskSpan(id string) obs.SpanContext {
	e.mu.Lock()
	span, ok := e.spans[id]
	e.mu.Unlock()
	if !ok {
		return obs.NewTrace()
	}
	return span
}

// wake signals every tasks/result goroutine waiting on id that the task may
// have reached a terminal status, and drops the cancel func and obs span. It
// runs after finish has emitted the task's terminal obs/v1 event, so dropping
// the span here does not lose the correlation.
func (e *Engine) wake(id string) {
	e.mu.Lock()
	chans := e.waiters[id]
	delete(e.waiters, id)
	delete(e.cancels, id)
	delete(e.spans, id)
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
// advertised only when it was opted in AND the deployment can identify
// requestors, honouring the vendored spec's rule that a receiver unable to
// identify requestors must not advertise it (RFC §8.5; brief 02 §4.5).
func (e *Engine) Capability() protocolcodec.TasksServerCapability {
	return protocolcodec.TasksServerCapability{
		List:      e.listOn,
		Cancel:    true,
		ToolsCall: true,
	}
}

// checkConcurrencyCap rejects a task creation that would push authContext over
// the per-requestor concurrent-task cap (RFC §8.5; brief 02 §4.6). A zero cap,
// or an unidentifiable requestor (empty authContext under an engine that does
// not identify requestors), is uncapped — the cap is a per-authorization-
// context limit and is meaningless without an identity. Non-terminal tasks
// count against the cap; terminal tasks have released their resources.
func (e *Engine) checkConcurrencyCap(ctx context.Context, authContext string) error {
	limit := e.life.MaxConcurrentPerRequestor
	if limit <= 0 {
		return nil
	}
	if authContext == "" && !e.identifiabl {
		return nil
	}
	active := 0
	cursor := ""
	for {
		recs, next, err := e.store.ListByAuthContext(ctx, authContext, cursor, 0)
		if err != nil {
			return fmt.Errorf("dockyard/runtime/tasks: concurrency-cap check: %w", err)
		}
		for _, r := range recs {
			if !r.Status.IsTerminal() {
				active++
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if active >= limit {
		return fmt.Errorf("%w: requestor has %d active tasks, the per-requestor cap is %d",
			ErrConcurrencyCap, active, limit)
	}
	return nil
}

// beginElicitation registers an outstanding input_required round-trip and moves
// the task to the input_required status so a tasks/get poller sees it is
// waiting (RFC §8.4). It is called by a taskHandle's RequireInput.
func (e *Engine) beginElicitation(ctx context.Context, id string, prompt InputPrompt, h *taskHandle) error {
	if _, err := e.store.Transition(ctx, id, protocolcodec.TaskInputRequired,
		prompt.Message); err != nil {
		return err
	}
	e.mu.Lock()
	e.elicits[id] = &elicitation{prompt: prompt, handle: h}
	e.mu.Unlock()
	return nil
}

// endElicitation clears an outstanding elicitation for id. It is idempotent.
func (e *Engine) endElicitation(id string) {
	e.mu.Lock()
	delete(e.elicits, id)
	e.mu.Unlock()
}

// PendingInput returns the prompt of the input_required elicitation outstanding
// on task id, and true, or a zero prompt and false when none is outstanding. It
// is the read side of the input_required round-trip — the transport mount or a
// test driver polls it to discover a task is waiting for input.
func (e *Engine) PendingInput(id string) (InputPrompt, bool) {
	e.mu.Lock()
	el, ok := e.elicits[id]
	e.mu.Unlock()
	if !ok {
		return InputPrompt{}, false
	}
	return el.prompt, true
}

// SupplyInput delivers a requestor's reply to the input_required elicitation
// outstanding on task id, unblocking the handler's RequireInput call. It
// returns ErrTaskNotFound when id names no task and ErrNoPendingInput when the
// task has no outstanding elicitation. It is the write side of the
// input_required round-trip (RFC §8.4).
func (e *Engine) SupplyInput(ctx context.Context, id string, resp InputResponse) error {
	if _, err := e.store.Get(ctx, id); err != nil {
		return err
	}
	e.mu.Lock()
	el, ok := e.elicits[id]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: task %q", ErrNoPendingInput, id)
	}
	if !el.handle.deliver(resp) {
		return fmt.Errorf("%w: task %q is not waiting on input", ErrNoPendingInput, id)
	}
	return nil
}
