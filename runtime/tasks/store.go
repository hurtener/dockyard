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
}

// Task projects the protocol-facing protocolcodec.Task from a record — the
// subset a host sees on the wire.
func (r TaskRecord) Task() protocolcodec.Task {
	return protocolcodec.Task{
		ID:            r.ID,
		Status:        r.Status,
		StatusMessage: r.StatusMessage,
		CreatedAt:     r.CreatedAt,
		LastUpdatedAt: r.UpdatedAt,
		TTL:           r.RequestedTTL,
		PollInterval:  r.PollInterval,
	}
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

	// SetResult records the terminal result of a task. It is called together
	// with the transition into a terminal status; it does not itself move the
	// lifecycle.
	SetResult(ctx context.Context, id string, result TaskResult) error

	// List returns a page of records and an opaque next-page cursor (empty when
	// the page is the last). An empty cursor requests the first page. limit
	// bounds the page size; a limit <= 0 uses the driver default.
	List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error)
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

// NewInMemoryStore returns an in-memory [TaskStore]. It is the Phase 13 default
// driver; Phase 14 adds the durable Store-backed driver behind the same seam.
func NewInMemoryStore() TaskStore {
	return &inMemoryStore{tasks: make(map[string]TaskRecord)}
}

func (s *inMemoryStore) Create(_ context.Context, rec TaskRecord) error {
	if rec.ID == "" {
		return fmt.Errorf("%w: task record has empty ID", ErrInvalidParams)
	}
	if rec.Status != protocolcodec.TaskWorking {
		return transitionError("", rec.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[rec.ID]; exists {
		return fmt.Errorf("dockyard/runtime/tasks: task %q already exists", rec.ID)
	}
	s.tasks[rec.ID] = rec
	s.order = append(s.order, rec.ID)
	return nil
}

func (s *inMemoryStore) Get(_ context.Context, id string) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	return rec, nil
}

func (s *inMemoryStore) Transition(
	_ context.Context, id string, to protocolcodec.TaskStatus, msg string,
) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	// A redundant write of the status the task already holds is a no-op
	// success — the cooperative-cancellation rule: a late terminal transition
	// onto an already-cancelled (or otherwise-terminal) task must not error.
	if rec.Status == to {
		return rec, nil
	}
	if !rec.Status.CanTransitionTo(to) {
		return TaskRecord{}, transitionError(rec.Status, to)
	}
	rec.Status = to
	rec.StatusMessage = msg
	rec.UpdatedAt = time.Now().UTC()
	s.tasks[id] = rec
	return rec, nil
}

func (s *inMemoryStore) SetResult(_ context.Context, id string, result TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	rec.Result = result
	s.tasks[id] = rec
	return nil
}

// defaultPageSize bounds an in-memory tasks/list page when the caller passes
// no explicit limit.
const defaultPageSize = 50

func (s *inMemoryStore) List(_ context.Context, cursor string, limit int) ([]TaskRecord, string, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()

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
		out = append(out, s.tasks[id])
	}
	return out, next, nil
}
