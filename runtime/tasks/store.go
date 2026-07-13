package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TaskResult is the durable outcome of a finished task — exactly what the
// underlying request would have returned (vendored spec, "Result Retrieval").
// For a tools/call task that is a CallToolResult. The Tasks engine stores it
// opaquely: tasks/result returns Payload verbatim when Err is empty, or a
// JSON-RPC error built from Err otherwise.
type TaskResult struct {
	// Payload is the underlying request's success result as raw JSON. Set when
	// the task completed successfully.
	Payload json.RawMessage
	// Err, when non-empty, is the human-readable failure message; the task is
	// in the failed status and tasks/result returns a JSON-RPC error.
	Err string
}

// InputMethod is one of the three client request methods permitted by the
// modern Tasks extension for mid-flight input.
type InputMethod string

const (
	// InputMethodElicitation requests structured user input.
	InputMethodElicitation InputMethod = protocolcodec.CoreMethodElicitation
	// InputMethodSampling requests an LLM completion.
	InputMethodSampling InputMethod = protocolcodec.CoreMethodSampling
	// InputMethodRoots requests the client's exposed roots.
	InputMethodRoots InputMethod = protocolcodec.CoreMethodRoots
)

// InputRequest is a persisted modern task input request. Payload is the request
// object consumed by the client; the codec owns its wire interpretation.
type InputRequest struct {
	Key     string
	Method  InputMethod
	Payload json.RawMessage
}

// TaskInputResponse is a persisted response to a modern task input request.
type TaskInputResponse struct {
	Payload json.RawMessage
}

// TaskRecord is the durable state of one task — the [TaskStore] row. It is the
// Dockyard-internal superset of protocolcodec.Task: it carries the lifecycle
// fields plus the bookkeeping the engine and Phase 14 need (the underlying
// request, the requested TTL, the auth context for Phase 14's binding, and the
// terminal result).
//
// The protocol-facing protocolcodec.Task is projected from a TaskRecord by
// [TaskRecord.Task]; raw experimental protocol structs never reach a TaskStore
// driver (P3).
type TaskRecord struct {
	// ID is the receiver-generated task identifier.
	ID string
	// Status is the current lifecycle status.
	Status protocolcodec.TaskStatus
	// StatusMessage is an optional human-readable status description.
	StatusMessage string
	// CreatedAt / UpdatedAt track the task's lifetime.
	CreatedAt time.Time
	UpdatedAt time.Time
	// RequestedTTL is the TTL the requestor asked for, in milliseconds; nil
	// means the requestor expressed no preference. Phase 14 turns this into the
	// enforced TTL; Phase 13 records it and reports it back unchanged.
	RequestedTTL *int64
	// TTL is the enforced retention duration in milliseconds — the value the
	// runtime actually honours after clamping RequestedTTL to the manifest max
	// and substituting the default (RFC §8.5). A nil TTL means unlimited
	// retention; the purge sweep never reaps a nil-TTL task. Phase 14 sets it;
	// it is the value [TaskRecord.Task] reports on the wire.
	TTL *int64
	// ExpiresAt is the absolute instant the task becomes eligible for the TTL
	// purge sweep, derived from CreatedAt + TTL. A zero ExpiresAt means the task
	// never expires (a nil TTL). Phase 14 sets it; the durable driver indexes
	// on it so PurgeExpired is a bounded scan.
	ExpiresAt time.Time
	// PollInterval is the receiver's suggested polling interval in ms; nil
	// omits it.
	PollInterval *int64
	// Method is the underlying request method the task wraps, e.g. "tools/call".
	Method string
	// ToolName is the tool a tools/call task wraps; empty for non-tool tasks.
	ToolName string
	// AuthContext is an opaque identifier for the requestor's authorization
	// context. Phase 13 records it; Phase 14 binds tasks/get|result|cancel and
	// scopes tasks/list to it. Empty means an unauthenticated requestor.
	AuthContext string
	// Result is the terminal outcome; meaningful only once Status is terminal.
	Result TaskResult
	// InputRequests are currently outstanding modern task input requests.
	InputRequests map[string]InputRequest
	// InputResponses retains accepted responses for the task lifetime. Retention
	// makes request keys single-use even after the worker consumes a response.
	InputResponses map[string]TaskInputResponse
}

// Task projects the protocol-facing protocolcodec.Task from a record — the
// subset a host sees on the wire. The wire `ttl` is the *enforced* TTL (the
// runtime-clamped value, RFC §8.5), falling back to the requested TTL only
// before Phase 14 enforcement has stamped one — so a host always sees the
// retention the runtime will actually honour.
func (r TaskRecord) Task() protocolcodec.Task {
	ttl := cloneInt64(r.TTL)
	if ttl == nil {
		ttl = cloneInt64(r.RequestedTTL)
	}
	return protocolcodec.Task{
		ID:            r.ID,
		Status:        r.Status,
		StatusMessage: r.StatusMessage,
		CreatedAt:     r.CreatedAt,
		LastUpdatedAt: r.UpdatedAt,
		TTL:           ttl,
		PollInterval:  cloneInt64(r.PollInterval),
	}
}

// IsExpired reports whether the task is eligible for the TTL purge sweep at
// instant now — its ExpiresAt is set and not in the future. A zero ExpiresAt
// (an unlimited-retention task) never expires.
func (r TaskRecord) IsExpired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}

// TaskStore is the persistence seam for durable task state — the interface
// + factory + driver pattern AGENTS.md §4.4 mandates for any subsystem with a
// plausible alternate backend.
//
// Phase 13 ships the in-memory driver ([NewInMemoryStore]); Phase 14 supplies
// the durable Store-backed driver (RFC §8.5) with TTL enforcement, concurrency
// caps and a purge sweep. The seam is deliberately shaped so Phase 14 plugs in
// without reshaping it: TaskRecord already carries AuthContext and RequestedTTL.
//
// A TaskStore must be safe for concurrent use by multiple goroutines — the
// Tasks engine dispatches concurrent requests against one store.
type TaskStore interface {
	// Create durably records a new task. The record's Status must be
	// TaskWorking — a task MUST begin in working (vendored spec, lifecycle
	// rule 1). It returns an error if a task with the same ID already exists.
	Create(ctx context.Context, rec TaskRecord) error

	// Get returns the record for id, or a wrapped ErrTaskNotFound.
	Get(ctx context.Context, id string) (TaskRecord, error)

	// Transition moves the task to status `to`, setting StatusMessage to msg
	// and stamping UpdatedAt. It returns the updated record. It returns a
	// wrapped ErrIllegalTransition if the move is not lifecycle-legal, and a
	// wrapped ErrTaskNotFound for an unknown id. A transition into the SAME
	// terminal status the task already holds is a no-op success (cancellation
	// is cooperative — a late terminal write on an already-cancelled task must
	// not error; vendored spec, "Task Cancellation" rule 3).
	Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (TaskRecord, error)

	// SetResult records a result without moving the lifecycle. Runtime terminal
	// paths use Finalize; this lower-level operation remains for store clients
	// that stage a non-terminal result explicitly.
	SetResult(ctx context.Context, id string, result TaskResult) error

	// AddInputRequest atomically persists a lifetime-unique request and moves the
	// task to input_required. A duplicate key returns ErrDuplicateInputKey.
	AddInputRequest(ctx context.Context, id string, req InputRequest) error

	// ApplyInputResponses atomically accepts responses for currently outstanding
	// keys. Unknown and previously satisfied keys are ignored. It returns the
	// accepted subset and the resulting record.
	ApplyInputResponses(ctx context.Context, id string, responses map[string]TaskInputResponse) (map[string]TaskInputResponse, TaskRecord, error)

	// List returns a page of records and an opaque next-page cursor (empty when
	// the page is the last). An empty cursor requests the first page. limit
	// bounds the page size; a limit <= 0 uses the driver default.
	List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error)

	// ListByAuthContext is List scoped to a single authorization context — the
	// only listing a receiver that identifies its requestors serves, so a
	// requestor sees its own tasks and no other context's (RFC §8.5; brief 02
	// §4.5). The page and cursor semantics match List. An empty authContext
	// scopes to the unauthenticated requestor's own (empty-context) tasks.
	ListByAuthContext(ctx context.Context, authContext, cursor string, limit int) ([]TaskRecord, string, error)

	// Delete removes a task record. It is a no-op (nil error) when the id names
	// no task — Delete is idempotent so the purge sweep can run without racing
	// a concurrent terminal write. It is the durable counterpart of letting an
	// in-memory record fall out of scope.
	Delete(ctx context.Context, id string) error

	// PurgeExpired reaps every terminal task whose enforced TTL has elapsed as
	// of now (TaskRecord.IsExpired) and returns the count removed. Active tasks
	// remain addressable so the engine can cancel and clean up their workers. It
	// is the storage half of the background TTL purge sweep (RFC §8.5); the
	// sweep goroutine lives in lifecycle.go. PurgeExpired is safe to call
	// concurrently with any other store operation.
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}

// AtomicCreateTaskStore is an optional TaskStore capability. Engines use it
// when available; legacy TaskStore implementations remain source-compatible
// and receive a process-wide serialized fallback when their dynamic value is
// comparable. Non-comparable legacy values are rejected by [NewEngine].
type AtomicCreateTaskStore interface {
	CreateWithConcurrencyLimit(ctx context.Context, rec TaskRecord, limit int) error
}

// AtomicFinalizeTaskStore is an optional TaskStore capability. Engines use it
// to commit a result and terminal status together. Legacy stores receive a
// process-wide serialized result-before-status fallback, so engines sharing a
// comparable store never observe a terminal task without its result.
type AtomicFinalizeTaskStore interface {
	Finalize(ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, result TaskResult) (rec TaskRecord, applied bool, err error)
}

// ExpiredActiveTaskStore is an optional TaskStore capability that returns one
// finite snapshot of active tasks whose enforced TTL has elapsed. Engines use
// it to avoid carrying offset cursors across destructive purge operations.
type ExpiredActiveTaskStore interface {
	ExpiredActive(ctx context.Context, now time.Time) ([]TaskRecord, error)
}

// CoordinationIdentityProvider is an optional TaskStore capability for stores
// whose dynamic value is not comparable or for facades that share one backend.
// TaskStoreCoordinationIdentity must always return the same non-nil comparable
// value for stores that share task lifecycle state. Comparable stores that do
// not implement this capability identify as their own dynamic value.
//
// Keeping this capability separate preserves TaskStore source compatibility.
type CoordinationIdentityProvider interface {
	TaskStoreCoordinationIdentity() any
}

// inMemoryStore is the Phase 13 in-memory TaskStore driver. It is sufficient
// for stdio single-user apps and for tests; it has no TTL sweep and no
// concurrency cap — those are Phase 14, on the durable driver.
//
// inMemoryStore is safe for concurrent use: every method takes the mutex.
type inMemoryStore struct {
	mu    sync.Mutex
	tasks map[string]TaskRecord
	order []string // insertion order, for stable cursor pagination
}

func (s *inMemoryStore) TaskStoreCoordinationIdentity() any { return s }

// NewInMemoryStore returns an in-memory [TaskStore]. It is the Phase 13 default
// driver; Phase 14 adds the durable Store-backed driver behind the same seam.
func NewInMemoryStore() TaskStore {
	return &inMemoryStore{tasks: make(map[string]TaskRecord)}
}

func (s *inMemoryStore) Create(ctx context.Context, rec TaskRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.createWithConcurrencyLimit(ctx, rec, 0)
}

func (s *inMemoryStore) CreateWithConcurrencyLimit(ctx context.Context, rec TaskRecord, limit int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.createWithConcurrencyLimit(ctx, rec, limit)
}

func (s *inMemoryStore) createWithConcurrencyLimit(ctx context.Context, rec TaskRecord, limit int) error {
	if rec.ID == "" {
		return fmt.Errorf("%w: task record has empty ID", ErrInvalidParams)
	}
	if rec.Status != protocolcodec.TaskWorking {
		return transitionError("", rec.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := s.tasks[rec.ID]; exists {
		return fmt.Errorf("dockyard/runtime/tasks: task %q already exists", rec.ID)
	}
	if limit > 0 {
		active := 0
		for _, existing := range s.tasks {
			if existing.AuthContext == rec.AuthContext && !existing.Status.IsTerminal() {
				active++
			}
		}
		if active >= limit {
			return fmt.Errorf("%w: requestor has %d active tasks, the per-requestor cap is %d", ErrConcurrencyCap, active, limit)
		}
	}
	s.tasks[rec.ID] = cloneTaskRecord(rec)
	s.order = append(s.order, rec.ID)
	return nil
}

func (s *inMemoryStore) Get(ctx context.Context, id string) (TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	return cloneTaskRecord(rec), nil
}

func (s *inMemoryStore) Transition(
	ctx context.Context, id string, to protocolcodec.TaskStatus, msg string,
) (TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	// A redundant write of the status the task already holds is a no-op
	// success — the cooperative-cancellation rule: a late terminal transition
	// onto an already-cancelled (or otherwise-terminal) task must not error. A
	// redundant *non-terminal* write (working→working) instead refreshes the
	// status message: that is how a TaskHandle reports progress without moving
	// the lifecycle (RFC §8.4).
	if rec.Status == to {
		if !to.IsTerminal() && msg != "" && msg != rec.StatusMessage {
			rec.StatusMessage = msg
			rec.UpdatedAt = time.Now().UTC()
			s.tasks[id] = rec
		}
		return cloneTaskRecord(rec), nil
	}
	if !rec.Status.CanTransitionTo(to) {
		return TaskRecord{}, transitionError(rec.Status, to)
	}
	rec.Status = to
	rec.StatusMessage = msg
	rec.UpdatedAt = time.Now().UTC()
	s.tasks[id] = rec
	return cloneTaskRecord(rec), nil
}

func (s *inMemoryStore) SetResult(ctx context.Context, id string, result TaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	rec.Result = TaskResult{Payload: append(json.RawMessage(nil), result.Payload...), Err: result.Err}
	s.tasks[id] = rec
	return nil
}

func (s *inMemoryStore) Finalize(
	ctx context.Context, id string, status protocolcodec.TaskStatus, msg string, result TaskResult,
) (TaskRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, false, err
	}
	if !status.IsTerminal() {
		return TaskRecord{}, false, transitionError("", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return TaskRecord{}, false, err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, false, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	if rec.Status.IsTerminal() {
		return cloneTaskRecord(rec), false, nil
	}
	if !rec.Status.CanTransitionTo(status) {
		return TaskRecord{}, false, transitionError(rec.Status, status)
	}
	rec.Status = status
	rec.StatusMessage = msg
	rec.Result = cloneTaskResult(result)
	rec.InputRequests = nil
	rec.UpdatedAt = time.Now().UTC()
	s.tasks[id] = rec
	return cloneTaskRecord(rec), true, nil
}

func (s *inMemoryStore) AddInputRequest(ctx context.Context, id string, req InputRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	if rec.Status.IsTerminal() {
		return transitionError(rec.Status, protocolcodec.TaskInputRequired)
	}
	if _, exists := rec.InputRequests[req.Key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateInputKey, req.Key)
	}
	if _, exists := rec.InputResponses[req.Key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateInputKey, req.Key)
	}
	if err := validateInputRequest(req); err != nil {
		return err
	}
	if rec.InputRequests == nil {
		rec.InputRequests = make(map[string]InputRequest)
	}
	rec.InputRequests[req.Key] = cloneInputRequest(req)
	rec.Status = protocolcodec.TaskInputRequired
	rec.UpdatedAt = time.Now().UTC()
	s.tasks[id] = rec
	return nil
}

func (s *inMemoryStore) ApplyInputResponses(ctx context.Context, id string, responses map[string]TaskInputResponse) (map[string]TaskInputResponse, TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, TaskRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, TaskRecord{}, err
	}
	rec, ok := s.tasks[id]
	if !ok {
		return nil, TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	if rec.Status != protocolcodec.TaskInputRequired {
		if rec.Status == protocolcodec.TaskWorking {
			return map[string]TaskInputResponse{}, cloneTaskRecord(rec), nil
		}
		return nil, cloneTaskRecord(rec), transitionError(rec.Status, protocolcodec.TaskInputRequired)
	}
	accepted := make(map[string]TaskInputResponse)
	if rec.InputResponses == nil {
		rec.InputResponses = make(map[string]TaskInputResponse)
	}
	for key, resp := range responses {
		if _, pending := rec.InputRequests[key]; !pending || len(resp.Payload) == 0 || !json.Valid(resp.Payload) {
			continue
		}
		resp.Payload = append(json.RawMessage(nil), resp.Payload...)
		rec.InputResponses[key] = resp
		accepted[key] = resp
		delete(rec.InputRequests, key)
	}
	if len(accepted) > 0 {
		if len(rec.InputRequests) == 0 && rec.Status == protocolcodec.TaskInputRequired {
			rec.Status = protocolcodec.TaskWorking
			rec.StatusMessage = "input received, resuming"
		}
		rec.UpdatedAt = time.Now().UTC()
		s.tasks[id] = rec
	}
	return accepted, cloneTaskRecord(rec), nil
}

func (m InputMethod) valid() bool {
	return m == InputMethodElicitation || m == InputMethodSampling || m == InputMethodRoots
}

func cloneInputRequest(req InputRequest) InputRequest {
	req.Payload = append(json.RawMessage(nil), req.Payload...)
	return req
}

func cloneTaskRecord(rec TaskRecord) TaskRecord {
	rec.RequestedTTL = cloneInt64(rec.RequestedTTL)
	rec.TTL = cloneInt64(rec.TTL)
	rec.PollInterval = cloneInt64(rec.PollInterval)
	rec.Result = cloneTaskResult(rec.Result)
	requests := rec.InputRequests
	responses := rec.InputResponses
	rec.InputRequests = make(map[string]InputRequest, len(requests))
	for key, req := range requests {
		rec.InputRequests[key] = cloneInputRequest(req)
	}
	rec.InputResponses = make(map[string]TaskInputResponse, len(responses))
	for key, resp := range responses {
		resp.Payload = append(json.RawMessage(nil), resp.Payload...)
		rec.InputResponses[key] = resp
	}
	return rec
}

func cloneTaskResult(result TaskResult) TaskResult {
	result.Payload = append(json.RawMessage(nil), result.Payload...)
	return result
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validateInputRequest(req InputRequest) error {
	if req.Key == "" || !req.Method.valid() || len(req.Payload) == 0 || !json.Valid(req.Payload) {
		return fmt.Errorf("%w: invalid input request", ErrInvalidParams)
	}
	if err := protocolcodec.ValidateModernInputRequest(string(req.Method), req.Payload); err != nil {
		return fmt.Errorf("%w: invalid input request: %w", ErrInvalidParams, err)
	}
	return nil
}

// defaultPageSize bounds an in-memory tasks/list page when the caller passes
// no explicit limit.
const defaultPageSize = 50

func (s *inMemoryStore) List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = defaultPageSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	// The cursor is the opaque, 1-past-the-end index into the stable insertion
	// order. Requestors MUST treat it as opaque (vendored spec, "Task Listing"
	// rule 3); it is decoded only here.
	start := 0
	if cursor != "" {
		i, err := decodeCursor(cursor)
		if err != nil || i < 0 || i > len(s.order) {
			return nil, "", fmt.Errorf("%w: bad cursor", ErrInvalidParams)
		}
		start = i
	}
	end := start + limit
	next := ""
	if end < len(s.order) {
		next = encodeCursor(end)
	} else {
		end = len(s.order)
	}
	out := make([]TaskRecord, 0, end-start)
	for _, id := range s.order[start:end] {
		out = append(out, cloneTaskRecord(s.tasks[id]))
	}
	return out, next, nil
}

// ListByAuthContext pages over the records whose AuthContext equals
// authContext, in stable insertion order. The cursor is a 1-past-the-end index
// into the *filtered* sequence — opaque to the caller, decoded only here.
func (s *inMemoryStore) ListByAuthContext(
	ctx context.Context, authContext, cursor string, limit int,
) ([]TaskRecord, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = defaultPageSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	// Build the auth-scoped view in insertion order.
	scoped := make([]TaskRecord, 0, len(s.order))
	for _, id := range s.order {
		if rec := s.tasks[id]; rec.AuthContext == authContext {
			scoped = append(scoped, cloneTaskRecord(rec))
		}
	}
	start := 0
	if cursor != "" {
		i, err := decodeCursor(cursor)
		if err != nil || i < 0 || i > len(scoped) {
			return nil, "", fmt.Errorf("%w: bad cursor", ErrInvalidParams)
		}
		start = i
	}
	end := start + limit
	next := ""
	if end < len(scoped) {
		next = encodeCursor(end)
	} else {
		end = len(scoped)
	}
	out := make([]TaskRecord, 0, end-start)
	out = append(out, scoped[start:end]...)
	return out, next, nil
}

// Delete removes a task from the in-memory store. It is idempotent: removing an
// absent task is a nil-error no-op.
func (s *inMemoryStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.tasks[id]; !ok {
		return nil
	}
	delete(s.tasks, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

// PurgeExpired reaps every record expired as of now and returns the count.
func (s *inMemoryStore) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	kept := s.order[:0:0]
	purged := 0
	for _, id := range s.order {
		rec := s.tasks[id]
		if rec.Status.IsTerminal() && rec.IsExpired(now) {
			delete(s.tasks, id)
			purged++
			continue
		}
		kept = append(kept, id)
	}
	s.order = kept
	return purged, nil
}

func (s *inMemoryStore) ExpiredActive(ctx context.Context, now time.Time) ([]TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]TaskRecord, 0)
	for _, id := range s.order {
		rec := s.tasks[id]
		if !rec.Status.IsTerminal() && rec.IsExpired(now) {
			out = append(out, cloneTaskRecord(rec))
		}
	}
	return out, nil
}
