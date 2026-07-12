package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	storeinmem "github.com/hurtener/dockyard/runtime/store/inmem"
)

func TestCreateToolTaskModernResultAndErrors(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	release := make(chan struct{})
	created, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName:    "report",
		AuthContext: "alice",
		Run:         blockingRun(release, json.RawMessage(`{"ok":true}`), nil),
	}, true)
	if err != nil {
		t.Fatalf("CreateToolTask: %v", err)
	}
	defer close(release)
	if created.ID == "" || created.Status != string(protocolcodec.TaskWorking) || !created.Required {
		t.Fatalf("created task = %#v", created)
	}
	if created.CreatedAt.IsZero() || created.LastUpdatedAt.IsZero() || created.PollInterval == nil {
		t.Fatalf("created task omitted lifecycle fields: %#v", created)
	}

	_, err = e.CreateToolTask(context.Background(), CreateToolCallParams{ToolName: "invalid"}, false)
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("invalid handler error = %v, want ErrInvalidParams", err)
	}
}

func TestTaskHandleModernMethodsAndFailures(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, err := NewEngine(store, &Options{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	rec := workingRecord("handle-modern")
	if err := store.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	h := &taskHandle{engine: e, id: rec.ID}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- h.RequestInput(ctx, InputRequest{
			Key: "roots", Method: InputMethodRoots,
			Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
		})
	}()
	waitForStatus(t, store, rec.ID, protocolcodec.TaskInputRequired)
	response := TaskInputResponse{Payload: json.RawMessage(`{"roots":[]}`)}
	if _, err := e.DispatchModern(ctx, "", MethodUpdate, ModernRequest{
		TaskID: rec.ID, InputResponses: map[string]TaskInputResponse{"roots": response},
	}); err != nil {
		t.Fatalf("DispatchModern update: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RequestInput: %v", err)
	}
	got, ok, err := h.ModernInputResponse(ctx, "roots")
	if err != nil || !ok || string(got.Payload) != string(response.Payload) {
		t.Fatalf("ModernInputResponse = (%s, %v, %v)", got.Payload, ok, err)
	}
	if _, ok, err := h.ModernInputResponse(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing ModernInputResponse = (ok %v, err %v)", ok, err)
	}

	missing := &taskHandle{engine: e, id: "missing"}
	if err := missing.Status(ctx, "no task"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Status missing error = %v", err)
	}
	if _, _, err := missing.ModernInputResponse(ctx, "x"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("ModernInputResponse missing error = %v", err)
	}
	if missing.Cancelled() {
		t.Fatal("missing task reported cancelled")
	}
}

func TestModernCancelSecurityContextAndBranches(t *testing.T) {
	t.Parallel()
	base := context.Background()
	if got := WithRequestAuthContext(base, ""); got != base {
		t.Fatal("empty auth context should preserve context")
	}
	ctx := WithRequestAuthContext(base, "alice")
	if got := RequestAuthContext(ctx); got != "alice" {
		t.Fatalf("RequestAuthContext = %q", got)
	}
	if got := RequestAuthContext(base); got != "" {
		t.Fatalf("empty RequestAuthContext = %q", got)
	}

	store := NewInMemoryStore()
	e, _ := NewEngine(store, &Options{Logger: quietLogger()})
	rec := workingRecord("cancel-modern")
	rec.AuthContext = "alice"
	if err := store.Create(base, rec); err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{})
	e.mu.Lock()
	e.cancels[rec.ID] = func() { close(cancelled) }
	e.mu.Unlock()
	if _, err := e.DispatchModern(base, "alice", MethodCancel, ModernRequest{TaskID: rec.ID}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("registered task cancellation was not invoked")
	}
	got, _ := store.Get(base, rec.ID)
	if got.Status != protocolcodec.TaskCancelled || got.Result.Err == "" {
		t.Fatalf("cancelled record = %#v", got)
	}
	if _, err := e.DispatchModern(base, "alice", MethodCancel, ModernRequest{TaskID: rec.ID}); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("second cancel error = %v", err)
	}
	if _, err := e.DispatchModern(base, "mallory", MethodGet, ModernRequest{TaskID: rec.ID}); !errors.Is(err, ErrCrossContext) {
		t.Fatalf("cross-context get error = %v", err)
	}
	if _, err := e.DispatchModern(base, "alice", MethodGet, ModernRequest{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty task ID error = %v", err)
	}
	if _, err := e.DispatchModern(base, "alice", MethodUpdate, ModernRequest{TaskID: rec.ID}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("nil responses error = %v", err)
	}
}

func TestDurableInputRequestsAndResponses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backing := storeinmem.New()
	defer func() { _ = backing.Close() }()
	if err := backing.Migrate(ctx, Migrations()); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(backing)
	if err != nil {
		t.Fatal(err)
	}
	rec := workingRecord("durable-input")
	if err := store.Create(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.AddInputRequest(ctx, rec.ID, InputRequest{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("invalid request error = %v", err)
	}
	req := InputRequest{Key: "approval", Method: InputMethodElicitation, Payload: json.RawMessage(`{"method":"elicitation/create","params":{"message":"Approve?"}}`)}
	if err := store.AddInputRequest(ctx, rec.ID, req); err != nil {
		t.Fatalf("AddInputRequest: %v", err)
	}
	if err := store.AddInputRequest(ctx, rec.ID, req); !errors.Is(err, ErrDuplicateInputKey) {
		t.Fatalf("duplicate request error = %v", err)
	}
	accepted, updated, err := store.ApplyInputResponses(ctx, rec.ID, map[string]TaskInputResponse{
		"unknown":  {Payload: json.RawMessage(`{"action":"accept"}`)},
		"approval": {Payload: json.RawMessage(`{"action":"accept"}`)},
	})
	if err != nil {
		t.Fatalf("ApplyInputResponses: %v", err)
	}
	if len(accepted) != 1 || updated.Status != protocolcodec.TaskWorking || len(updated.InputRequests) != 0 {
		t.Fatalf("durable update = accepted %#v, record %#v", accepted, updated)
	}
	persisted, err := store.Get(ctx, rec.ID)
	if err != nil || len(persisted.InputResponses) != 1 {
		t.Fatalf("persisted response = %#v, %v", persisted, err)
	}
	if err := store.AddInputRequest(ctx, rec.ID, req); !errors.Is(err, ErrDuplicateInputKey) {
		t.Fatalf("reused response key error = %v", err)
	}
	if _, _, err := store.ApplyInputResponses(ctx, "missing", nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task response error = %v", err)
	}
}
