package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
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
	// MethodSupplyInput is a Dockyard-internal extension method (Phase 25 /
	// D-134) that delivers an `input_required` elicitation response to a
	// suspended task — the wire half of [Engine.SupplyInput]. The vendored
	// experimental Tasks spec does not define a standard wire shape for
	// resuming an input_required task (the SDK-typed surface is engine-
	// internal); the inspector needs one to forward an App's
	// elicitation-response over HTTP. The method name is namespaced under
	// `dockyard/` so it cannot be confused with a future standard spec
	// method — a deliberate vendor prefix per RFC §16.
	MethodSupplyInput = "dockyard/tasks/supplyInput"
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
	identity    any         // stable comparable task lifecycle coordination key
	storeMu     *sync.Mutex // shared across engines using the same comparable legacy TaskStore
	purgeMu     sync.Mutex

	mu           sync.Mutex
	cancels      map[string]context.CancelFunc                  // taskID → cancel of its run goroutine
	waiters      map[string][]chan struct{}                     // taskID → terminal-status signal channels
	elicits      map[string]*elicitation                        // taskID → outstanding input_required prompt
	inputWaiters map[string]map[string][]chan TaskInputResponse // taskID → request key → waiters
	spans        map[string]obs.SpanContext                     // taskID → obs/v1 trace span correlating its events

	sweep *purgeSweep // background TTL purge sweep; nil when no interval set
}

var legacyStoreLocks sync.Map // map[TaskStore]*sync.Mutex

type taskOwnerKey struct {
	store any
	id    string
}

type taskOwner struct {
	mu     sync.Mutex
	engine *Engine
	ready  chan struct{}
	active bool
}

var taskOwners = struct {
	sync.Mutex
	owners map[taskOwnerKey]*taskOwner
}{owners: make(map[taskOwnerKey]*taskOwner)}

func coordinationIdentity(store TaskStore) (any, error) {
	identity := any(store)
	if provider, ok := store.(CoordinationIdentityProvider); ok {
		identity = provider.TaskStoreCoordinationIdentity()
		if !validCoordinationIdentity(identity) {
			return nil, fmt.Errorf("dockyard/runtime/tasks: TaskStore %T CoordinationIdentityProvider returned a nil or non-comparable identity", store)
		}
		return identity, nil
	}
	if !validCoordinationIdentity(identity) {
		return nil, fmt.Errorf("dockyard/runtime/tasks: TaskStore %T has no stable comparable coordination identity; non-comparable stores must implement CoordinationIdentityProvider", store)
	}
	return identity, nil
}

func validCoordinationIdentity(identity any) bool {
	if identity == nil {
		return false
	}
	value := reflect.ValueOf(identity)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return false
		}
	case reflect.UnsafePointer:
		if value.Pointer() == 0 {
			return false
		}
	}
	return value.Comparable()
}

func taskOwnerForIdentity(identity any, id string) *taskOwner {
	if identity == nil {
		return nil
	}
	key := taskOwnerKey{store: identity, id: id}
	taskOwners.Lock()
	owner := taskOwners.owners[key]
	taskOwners.Unlock()
	return owner
}

// taskOwnerFor is retained for package tests that inspect registry cleanup.
func taskOwnerFor(store TaskStore, id string) *taskOwner {
	identity, err := coordinationIdentity(store)
	if err != nil {
		return nil
	}
	return taskOwnerForIdentity(identity, id)
}

func unregisterTaskOwner(identity any, id string, engine *Engine) {
	if identity == nil {
		return
	}
	key := taskOwnerKey{store: identity, id: id}
	taskOwners.Lock()
	if owner := taskOwners.owners[key]; owner != nil && owner.engine == engine {
		delete(taskOwners.owners, key)
	}
	taskOwners.Unlock()
}

func reserveTaskOwner(identity any, id string, engine *Engine) (*taskOwner, bool) {
	key := taskOwnerKey{store: identity, id: id}
	owner := &taskOwner{engine: engine, ready: make(chan struct{})}
	taskOwners.Lock()
	if _, exists := taskOwners.owners[key]; exists {
		taskOwners.Unlock()
		return nil, false
	}
	taskOwners.owners[key] = owner
	taskOwners.Unlock()
	return owner, true
}

func releaseTaskOwnerReservation(identity any, id string, owner *taskOwner) {
	key := taskOwnerKey{store: identity, id: id}
	taskOwners.Lock()
	released := false
	if taskOwners.owners[key] == owner {
		delete(taskOwners.owners, key)
		released = true
	}
	taskOwners.Unlock()
	if released {
		close(owner.ready)
	}
}

func activateTaskOwner(owner *taskOwner) {
	if owner == nil {
		return
	}
	owner.active = true
	close(owner.ready)
}

func awaitTaskOwner(ctx context.Context, owner *taskOwner) (bool, error) {
	if owner == nil {
		return false, nil
	}
	select {
	case <-owner.ready:
		return owner.active, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// taskRuntimeFor returns the engine holding id's process-local lifecycle state.
// A missing or released owner falls back to the receiver; durable state remains
// authoritative when a task was restored or its owner has already exited.
func (e *Engine) taskRuntimeFor(ctx context.Context, id string) (*Engine, error) {
	owner := taskOwnerForIdentity(e.identity, id)
	active, err := awaitTaskOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	if active && owner.engine != nil {
		return owner.engine, nil
	}
	return e, nil
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
	identity, err := coordinationIdentity(store)
	if err != nil {
		return nil, err
	}
	var storeMu *sync.Mutex
	_, atomicCreate := store.(AtomicCreateTaskStore)
	_, atomicFinalize := store.(AtomicFinalizeTaskStore)
	if !atomicCreate || !atomicFinalize {
		lock, _ := legacyStoreLocks.LoadOrStore(identity, &sync.Mutex{})
		storeMu = lock.(*sync.Mutex)
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
		store:        store,
		codec:        protocolcodec.CodecFor(protocolcodec.DefaultVersion),
		log:          log,
		rec:          obs.NewRecorder(emitter, serverID),
		genID:        genID,
		pollMS:       pollMS,
		listOn:       listOn && identifiable,
		identifiabl:  identifiable,
		life:         life,
		identity:     identity,
		storeMu:      storeMu,
		cancels:      make(map[string]context.CancelFunc),
		waiters:      make(map[string][]chan struct{}),
		elicits:      make(map[string]*elicitation),
		inputWaiters: make(map[string]map[string][]chan TaskInputResponse),
		spans:        make(map[string]obs.SpanContext),
	}
	e.sweep = newPurgeSweep(store, life.PurgeInterval, log)
	if e.sweep != nil {
		e.sweep.run = e.purgeExpired
	}
	return e, nil
}

// purgeExpired terminalizes expired active tasks before deleting expired
// terminal records so worker cleanup and concurrency-cap accounting agree with
// durable retention.
func (e *Engine) purgeExpired(ctx context.Context, now time.Time) (int, error) {
	// Serialize scans so overlapping manual and background sweeps cannot do
	// duplicate cancellation work within this engine.
	e.purgeMu.Lock()
	defer e.purgeMu.Unlock()

	expired, err := e.expiredActiveSnapshot(ctx, now)
	if err != nil {
		return 0, err
	}
	for _, rec := range expired {
		owner := taskOwnerForIdentity(e.identity, rec.ID)
		active, err := awaitTaskOwner(ctx, owner)
		if err != nil {
			return 0, err
		}
		if !active {
			owner = nil
		}
		engine := e
		if owner != nil {
			engine = owner.engine
		}
		_, _, err = engine.cancelTask(ctx, rec.ID,
			"The task expired.", TaskResult{Err: "task expired"}, owner)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				continue // concurrently deleted after the snapshot
			}
			return 0, err
		}
	}
	return e.store.PurgeExpired(ctx, now)
}

func (e *Engine) expiredActiveSnapshot(ctx context.Context, now time.Time) ([]TaskRecord, error) {
	if scanner, ok := e.store.(ExpiredActiveTaskStore); ok {
		return scanner.ExpiredActive(ctx, now)
	}
	// Legacy stores remain compatible. Their List contract provides stable pages
	// when the caller does not mutate between reads; destructive purge starts
	// only after this complete scan.
	var expired []TaskRecord
	cursor := ""
	for {
		recs, next, err := e.store.List(ctx, cursor, 0)
		if err != nil {
			return nil, err
		}
		for _, rec := range recs {
			if !rec.Status.IsTerminal() && rec.IsExpired(now) {
				expired = append(expired, rec)
			}
		}
		if next == "" {
			return expired, nil
		}
		cursor = next
	}
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

// CreatedTask is the protocol-neutral result of accepting work as a task.
// App-facing packages carry this value; only the server edge selects and
// encodes a versioned CreateTaskResult wire shape.
type CreatedTask struct {
	ID            string
	Status        string
	StatusMessage string
	CreatedAt     time.Time
	LastUpdatedAt time.Time
	TTL           *int64
	PollInterval  *int64
	Required      bool
}

// CreateToolTask is the domain-returning task creation API used by tool
// handlers. Required means a modern caller that did not advertise the Tasks
// extension must receive the standard missing-required-capability error rather
// than the handler's ordinary fallback result.
func (e *Engine) CreateToolTask(ctx context.Context, p CreateToolCallParams, required bool) (CreatedTask, error) {
	created, err := e.createForToolCallWithRequired(ctx, p, &required)
	if err != nil {
		return CreatedTask{}, err
	}
	return CreatedTask{
		ID: created.ID, Status: string(created.Status), StatusMessage: created.StatusMessage,
		CreatedAt: created.CreatedAt, LastUpdatedAt: created.LastUpdatedAt,
		TTL: created.TTL, PollInterval: created.PollInterval, Required: required,
	}, nil
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
	// This legacy API returns its already-encoded task result directly rather
	// than handing a CreatedTask to the server response middleware. It therefore
	// remains immediate even when called from a tools/call carrying deferred
	// admission; only CreateToolTask participates in that handoff.
	created, err := e.createForToolCall(withoutDeferredAdmission(ctx), p)
	if err != nil {
		return nil, err
	}
	return e.codec.EncodeCreateTaskResult(protocolcodec.CreateTaskResult{Task: created})
}

func (e *Engine) createForToolCall(ctx context.Context, p CreateToolCallParams) (protocolcodec.Task, error) {
	return e.createForToolCallWithRequired(ctx, p, nil)
}

func (e *Engine) createForToolCallWithRequired(ctx context.Context, p CreateToolCallParams, required *bool) (protocolcodec.Task, error) {
	if (p.Run == nil) == (p.Handle == nil) {
		return protocolcodec.Task{}, fmt.Errorf("%w: CreateForToolCall requires exactly one of Run or Handle", ErrInvalidParams)
	}
	if verified, ok := requestAuthContext(ctx); ok {
		p.AuthContext = verified
	}

	id, err := e.genID()
	if err != nil {
		return protocolcodec.Task{}, err
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
	limit := e.life.MaxConcurrentPerRequestor
	if p.AuthContext == "" && !e.identifiabl {
		limit = 0
	}
	owner, err := e.createTask(ctx, rec, limit)
	if err != nil {
		return protocolcodec.Task{}, fmt.Errorf("dockyard/runtime/tasks: create task: %w", err)
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
	// scrubDetached strips request-scoped credentials (e.g. an exposed
	// delegation token) that must not outlive the request into the async run.
	runCtx, cancel := context.WithCancel(scrubDetached(context.WithoutCancel(ctx)))
	// One obs/v1 trace span correlates the whole task lifecycle: the start
	// event here, any progress events, and the terminal event in finish all
	// share this span (RFC §11.2 — W3C Trace Context correlation).
	span := obs.NewTrace()
	e.mu.Lock()
	e.cancels[id] = cancel
	e.spans[id] = span
	e.mu.Unlock()
	activateTaskOwner(owner)

	// runTask owns runCtx for its whole lifetime and always releases cancel on
	// exit; tasks/cancel may also call it early. Calling a CancelFunc twice is
	// safe, so both paths are correct.
	start := func(startCtx context.Context, validateAdmission bool) error {
		if validateAdmission {
			// Admission and lifecycle cancellation share e.mu. Verify the durable
			// record and launch while holding it so a sweep cannot cancel/delete the
			// task between validation and the handler becoming runnable.
			e.mu.Lock()
			defer e.mu.Unlock()
			current, err := e.store.Get(startCtx, id)
			if err != nil {
				return fmt.Errorf("dockyard/runtime/tasks: admit task %q: %w", id, err)
			}
			if current.Status != protocolcodec.TaskWorking {
				return fmt.Errorf("dockyard/runtime/tasks: admit task %q: task is in status %q", id, current.Status)
			}
			if current.IsExpired(time.Now().UTC()) {
				return fmt.Errorf("dockyard/runtime/tasks: admit task %q: task expired before admission", id)
			}
			if runCtx.Err() != nil {
				return fmt.Errorf("dockyard/runtime/tasks: admit task %q: %w", id, runCtx.Err())
			}
		}
		// Admission is now final: publish the lifecycle and only then allow app
		// handler side effects to begin.
		e.rec.TaskEvent(ctx, span, obs.PhaseStart, obs.TaskProgressPayload{
			TaskID: id,
			Status: string(protocolcodec.TaskWorking),
			Tool:   p.ToolName,
		}, nil)
		e.log.InfoContext(ctx, "task created",
			slog.String("taskId", id), slog.String("tool", p.ToolName))
		go e.runTask(runCtx, cancel, id, run)
		return nil
	}
	abort := func(abortCtx context.Context) error {
		if owner != nil {
			owner.mu.Lock()
			defer owner.mu.Unlock()
		}
		cancel()
		// All tasks rejected by one tool call share the server edge's cleanup
		// deadline. Finalization and deletion race within that same budget so
		// either successful path prevents an active orphan.
		finalizeDone := make(chan error, 1)
		deleteDone := make(chan error, 1)
		go func() {
			_, _, err := e.finalizeTask(abortCtx, id, protocolcodec.TaskCancelled,
				"Task admission was aborted.", TaskResult{Err: "task admission aborted"})
			finalizeDone <- err
		}()
		go func() { deleteDone <- e.store.Delete(abortCtx, id) }()
		finalizeErr, deleteErr := <-finalizeDone, <-deleteDone
		if deleteErr == nil || errors.Is(deleteErr, ErrTaskNotFound) {
			e.wake(id)
			return nil
		}
		if finalizeErr == nil || errors.Is(finalizeErr, ErrTaskNotFound) {
			e.wake(id)
		}
		return errors.Join(deleteErr, finalizeErr)
	}
	if admission := deferredAdmissionFromContext(ctx); admission != nil {
		canonical := CreatedTask{
			ID: rec.ID, Status: string(rec.Status), StatusMessage: rec.StatusMessage,
			CreatedAt: rec.CreatedAt, LastUpdatedAt: rec.UpdatedAt,
			TTL: rec.TTL, PollInterval: rec.PollInterval,
		}
		if required != nil {
			canonical.Required = *required
		}
		if err := admission.add(id, canonical, func(startCtx context.Context) error {
			return start(startCtx, true)
		}, abort); err != nil {
			return protocolcodec.Task{}, fmt.Errorf("defer task admission: %w", err)
		}
	} else {
		if err := start(ctx, false); err != nil {
			return protocolcodec.Task{}, errors.Join(err, abort(ctx))
		}
	}

	return rec.Task(), nil
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
// Finalize commits the result and terminal status in one store operation. A
// tasks/result waiter therefore cannot observe a terminal status paired with
// an empty or losing-racer result.
func (e *Engine) finish(
	ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, res TaskResult,
) {
	owner := taskOwnerForIdentity(e.identity, id)
	if owner != nil && owner.engine == e {
		owner.mu.Lock()
		defer owner.mu.Unlock()
	}
	_, applied, err := e.finalizeTask(ctx, id, status, msg, res)
	if err != nil {
		e.log.ErrorContext(ctx, "task finish failed",
			slog.String("taskId", id), slog.String("error", err.Error()))
		if errors.Is(err, ErrTaskNotFound) {
			e.wake(id)
		}
		return
	}
	if !applied {
		e.wake(id)
		return
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

func (e *Engine) createTask(ctx context.Context, rec TaskRecord, limit int) (*taskOwner, error) {
	reservation, reserved := reserveTaskOwner(e.identity, rec.ID, e)
	if !reserved {
		return nil, fmt.Errorf("dockyard/runtime/tasks: generated task ID %q is already reserved", rec.ID)
	}
	created := false
	defer func() {
		if !created {
			releaseTaskOwnerReservation(e.identity, rec.ID, reservation)
		}
	}()
	var err error
	if atomic, ok := e.store.(AtomicCreateTaskStore); ok {
		err = atomic.CreateWithConcurrencyLimit(ctx, rec, limit)
	} else {
		if e.storeMu == nil {
			return nil, errors.New("dockyard/runtime/tasks: legacy TaskStore synchronization is unavailable")
		}
		e.storeMu.Lock()
		defer e.storeMu.Unlock()
		if limit > 0 {
			active := 0
			cursor := ""
			for {
				recs, next, listErr := e.store.ListByAuthContext(ctx, rec.AuthContext, cursor, 0)
				if listErr != nil {
					return nil, listErr
				}
				for _, existing := range recs {
					if !existing.Status.IsTerminal() {
						active++
					}
				}
				if next == "" {
					break
				}
				cursor = next
			}
			if active >= limit {
				return nil, fmt.Errorf("%w: requestor has %d active tasks, the per-requestor cap is %d", ErrConcurrencyCap, active, limit)
			}
		}
		err = e.store.Create(ctx, rec)
	}
	created = err == nil
	if err != nil {
		return nil, err
	}
	return reservation, nil
}

func (e *Engine) finalizeTask(
	ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, result TaskResult,
) (TaskRecord, bool, error) {
	if atomic, ok := e.store.(AtomicFinalizeTaskStore); ok {
		return atomic.Finalize(ctx, id, status, msg, result)
	}
	if e.storeMu == nil {
		return TaskRecord{}, false, errors.New("dockyard/runtime/tasks: legacy TaskStore synchronization is unavailable")
	}
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	rec, err := e.store.Get(ctx, id)
	if err != nil {
		return TaskRecord{}, false, err
	}
	if rec.Status.IsTerminal() {
		return rec, false, nil
	}
	if !status.IsTerminal() || !rec.Status.CanTransitionTo(status) {
		return TaskRecord{}, false, transitionError(rec.Status, status)
	}
	if err := e.store.SetResult(ctx, id, result); err != nil {
		return TaskRecord{}, false, err
	}
	rec, err = e.store.Transition(ctx, id, status, msg)
	return rec, err == nil, err
}

// finalizeCancellation serializes terminal cancellation with legacy input
// delivery. Once cancellation commits, no stale elicitation can be claimed.
func (e *Engine) finalizeCancellation(
	ctx context.Context, id, msg string, result TaskResult,
) (TaskRecord, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, applied, err := e.finalizeTask(ctx, id, protocolcodec.TaskCancelled, msg, result)
	if err == nil && rec.Status.IsTerminal() {
		delete(e.elicits, id)
	}
	return rec, applied, err
}

// cancelTask owns the complete cancellation terminal section. When owner is
// non-nil, callers from any engine serialize with the engine that created the
// task before touching its worker, observability span, or waiters.
func (e *Engine) cancelTask(
	ctx context.Context, id, msg string, result TaskResult, owner *taskOwner,
) (TaskRecord, bool, error) {
	if owner != nil && owner.engine != e {
		return owner.engine.cancelTask(ctx, id, msg, result, owner)
	}
	if owner != nil {
		owner.mu.Lock()
		defer owner.mu.Unlock()
	}
	rec, applied, err := e.finalizeCancellation(ctx, id, msg, result)
	if err != nil || !applied {
		return rec, applied, err
	}
	e.mu.Lock()
	cancel := e.cancels[id]
	e.mu.Unlock()
	span := e.taskSpan(id).Child()
	if cancel != nil {
		cancel()
	}
	e.rec.TaskEvent(ctx, span, obs.PhaseEnd, obs.TaskProgressPayload{
		TaskID: id, Status: string(protocolcodec.TaskCancelled), Message: msg,
	}, nil)
	e.wake(id)
	return rec, true, nil
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
	delete(e.inputWaiters, id)
	delete(e.spans, id)
	e.mu.Unlock()
	unregisterTaskOwner(e.identity, id, e)
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

func (e *Engine) removeWaiter(id string, target chan struct{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	waiters := e.waiters[id]
	for i, waiter := range waiters {
		if waiter == target {
			waiters = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(waiters) == 0 {
		delete(e.waiters, id)
	} else {
		e.waiters[id] = waiters
	}
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

// beginElicitation registers an outstanding input_required round-trip and moves
// the task to the input_required status so a tasks/get poller sees it is
// waiting (RFC §8.4). It is called by a taskHandle's RequireInput.
func (e *Engine) beginElicitation(ctx context.Context, id string, prompt InputPrompt, h *taskHandle) error {
	pending := &elicitation{prompt: prompt, handle: h}
	e.mu.Lock()
	e.elicits[id] = pending
	e.mu.Unlock()

	if _, err := e.store.Transition(ctx, id, protocolcodec.TaskInputRequired,
		prompt.Message); err != nil {
		e.mu.Lock()
		if e.elicits[id] == pending {
			delete(e.elicits, id)
		}
		e.mu.Unlock()
		return err
	}
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
	runtime, err := e.taskRuntimeFor(ctx, id)
	if err != nil {
		return err
	}
	return runtime.supplyInput(ctx, id, resp)
}

func (e *Engine) supplyInput(ctx context.Context, id string, resp InputResponse) error {
	rec, err := e.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if rec.Status != protocolcodec.TaskInputRequired {
		return fmt.Errorf("%w: task %q is in status %q", ErrNoPendingInput, id, rec.Status)
	}
	e.mu.Lock()
	el, ok := e.elicits[id]
	if ok {
		delete(e.elicits, id)
	}
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: task %q", ErrNoPendingInput, id)
	}
	if !el.handle.deliver(resp) {
		return fmt.Errorf("%w: task %q is not waiting on input", ErrNoPendingInput, id)
	}
	return nil
}
