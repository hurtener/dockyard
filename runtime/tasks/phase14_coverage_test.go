package tasks

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestLifecycleFromMillis maps the manifest's millisecond-denominated tasks
// block onto the engine's Lifecycle (RFC §8.5). A zero millisecond value maps
// to a zero Duration — "no limit".
func TestLifecycleFromMillis(t *testing.T) {
	t.Parallel()
	got := LifecycleFromMillis(3600000, 900000, 60000, 16)
	if got.MaxTTL != time.Hour {
		t.Errorf("MaxTTL = %v, want 1h", got.MaxTTL)
	}
	if got.DefaultTTL != 15*time.Minute {
		t.Errorf("DefaultTTL = %v, want 15m", got.DefaultTTL)
	}
	if got.PurgeInterval != time.Minute {
		t.Errorf("PurgeInterval = %v, want 1m", got.PurgeInterval)
	}
	if got.MaxConcurrentPerRequestor != 16 {
		t.Errorf("MaxConcurrentPerRequestor = %d, want 16", got.MaxConcurrentPerRequestor)
	}
	// Zero millisecond values map to zero Durations — no limit.
	zero := LifecycleFromMillis(0, 0, 0, 0)
	if zero != (Lifecycle{}) {
		t.Errorf("all-zero millis must map to the zero Lifecycle, got %+v", zero)
	}
	// A negative value is also treated as "no limit".
	neg := LifecycleFromMillis(-1, -1, -1, 0)
	if neg.MaxTTL != 0 || neg.DefaultTTL != 0 || neg.PurgeInterval != 0 {
		t.Errorf("negative millis must map to zero Durations, got %+v", neg)
	}
}

// TestTaskHandle_StatusVerbatim covers TaskHandle.Status — it sets the status
// message verbatim, without a percentage prefix.
func TestTaskHandle_StatusVerbatim(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	reported := make(chan struct{})
	release := make(chan struct{})
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		if err := h.Status(ctx, "contacting upstream"); err != nil {
			return nil, err
		}
		close(reported)
		<-release
		return json.RawMessage(`{"isError":false}`), nil
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	<-reported
	getRaw, err := e.Dispatch(ctx, MethodGet, mustTaskIDParams(t, res.Task.ID))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	polled, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(getRaw)
	if polled.StatusMessage != "contacting upstream" {
		t.Fatalf("Status message = %q, want verbatim", polled.StatusMessage)
	}
	close(release)
	if _, err := e.Dispatch(ctx, MethodResult, mustTaskIDParams(t, res.Task.ID)); err != nil {
		t.Fatalf("tasks/result: %v", err)
	}
}

// TestTaskHandle_CancelledViaStatus covers TaskHandle.Cancelled observing the
// task's cancelled status (the non-ctx path).
func TestTaskHandle_CancelledViaStatus(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	observed := make(chan bool, 1)
	gate := make(chan struct{})
	handler := func(_ context.Context, h TaskHandle) (json.RawMessage, error) {
		<-gate
		// h.ctx is the run context; use a fresh handle whose ctx is not yet
		// cancelled so Cancelled() must consult the store.
		fresh := &taskHandle{engine: h.(*taskHandle).engine, id: h.(*taskHandle).id}
		observed <- fresh.Cancelled()
		return json.RawMessage(`{}`), nil
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if _, err := e.Dispatch(ctx, MethodCancel, mustTaskIDParams(t, res.Task.ID)); err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	close(gate)
	if got := <-observed; !got {
		t.Fatal("TaskHandle.Cancelled did not observe the cancelled status via the store")
	}
}

// TestInMemoryStore_Delete covers the in-memory store's Delete — idempotent
// removal — and that a deleted task is gone from List.
func TestInMemoryStore_Delete(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Create(ctx, TaskRecord{
			ID: id, Status: protocolcodec.TaskWorking,
			CreatedAt: now, UpdatedAt: now, Method: "tools/call",
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	if err := s.Delete(ctx, "b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "b"); err == nil {
		t.Fatal("deleted task is still gettable")
	}
	// Delete is idempotent.
	if err := s.Delete(ctx, "b"); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if err := s.Delete(ctx, "never"); err != nil {
		t.Fatalf("Delete of an unknown task: %v", err)
	}
	all, _, err := s.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("after Delete, List has %d tasks, want 2", len(all))
	}
}

// TestInMemoryStore_ListByAuthContext_Paginates covers the auth-scoped listing
// cursor path on the in-memory store.
func TestInMemoryStore_ListByAuthContext_Paginates(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	for i, id := range []string{"a1", "a2", "a3", "b1"} {
		authCtx := "alice"
		if id[0] == 'b' {
			authCtx = "bob"
		}
		if err := s.Create(ctx, TaskRecord{
			ID: id, Status: protocolcodec.TaskWorking,
			CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
			UpdatedAt: now, AuthContext: authCtx, Method: "tools/call",
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	// Page alice's 3 tasks two at a time.
	page1, next, err := s.ListByAuthContext(ctx, "alice", "", 2)
	if err != nil {
		t.Fatalf("ListByAuthContext page 1: %v", err)
	}
	if len(page1) != 2 || next == "" {
		t.Fatalf("page 1: len=%d next=%q", len(page1), next)
	}
	page2, next2, err := s.ListByAuthContext(ctx, "alice", next, 2)
	if err != nil {
		t.Fatalf("ListByAuthContext page 2: %v", err)
	}
	if len(page2) != 1 || next2 != "" {
		t.Fatalf("page 2 (last): len=%d next=%q", len(page2), next2)
	}
	// A bad cursor is a typed error.
	if _, _, err := s.ListByAuthContext(ctx, "alice", "garbage", 2); err == nil {
		t.Fatal("ListByAuthContext must reject a bad cursor")
	}
}

// TestJSONRPCCode_Phase14Errors covers the JSON-RPC code mapping for the
// Phase 14 sentinel errors.
func TestJSONRPCCode_Phase14Errors(t *testing.T) {
	t.Parallel()
	for _, err := range []error{ErrConcurrencyCap, ErrNoPendingInput, ErrCrossContext} {
		if code := JSONRPCCode(err); code != CodeInvalidParams {
			t.Errorf("JSONRPCCode(%v) = %d, want %d", err, code, CodeInvalidParams)
		}
	}
	if JSONRPCCode(nil) != 0 {
		t.Error("JSONRPCCode(nil) must be 0")
	}
}

// TestTaskHandle_ProgressRejectedAfterTerminal proves a Progress call on a task
// that has left the working status is a typed error, not an illegal transition.
func TestTaskHandle_ProgressRejectedAfterTerminal(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	store := e.store
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.Create(ctx, TaskRecord{
		ID: "t", Status: protocolcodec.TaskWorking,
		CreatedAt: now, UpdatedAt: now, Method: "tools/call",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Transition(ctx, "t", protocolcodec.TaskCompleted, "done"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	h := &taskHandle{engine: e, id: "t"}
	if err := h.Progress(ctx, 0.5, "late"); err == nil {
		t.Fatal("Progress on a completed task must be a typed error")
	}
}
