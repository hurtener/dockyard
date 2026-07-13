package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	storeinmem "github.com/hurtener/dockyard/runtime/store/inmem"
)

func sharedBackendStores(t *testing.T) (TaskStore, TaskStore) {
	t.Helper()
	backing := storeinmem.New()
	t.Cleanup(func() { _ = backing.Close() })
	if err := backing.Migrate(context.Background(), Migrations()); err != nil {
		t.Fatal(err)
	}
	first, err := NewStore(backing)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(backing)
	if err != nil {
		t.Fatal(err)
	}
	return first, second
}

func TestSharedBackendResultWaitsOnOwningEngine(t *testing.T) {
	ownerStore, peerStore := sharedBackendStores(t)
	release := make(chan struct{})
	started := make(chan struct{})
	owner, err := NewEngine(ownerStore, &Options{GenerateID: func() (string, error) { return "shared-result", nil }})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := NewEngine(peerStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "shared", AuthContext: "alice",
		Run: func(context.Context) (json.RawMessage, error) {
			close(started)
			<-release
			return json.RawMessage(`{"content":[]}`), nil
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	<-started

	done := make(chan error, 1)
	params := mustTaskIDParams(t, created.ID)
	go func() {
		_, err := peer.DispatchAs(context.Background(), "alice", MethodResult, params)
		done <- err
	}()
	waitForEngineWaiter(t, owner, created.ID)
	peer.mu.Lock()
	peerWaiters := len(peer.waiters[created.ID])
	peer.mu.Unlock()
	if peerWaiters != 0 {
		t.Fatalf("peer retained %d result waiters", peerWaiters)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer tasks/result did not wake on owner completion")
	}
}

func TestSharedBackendModernInputReachesOwningEngine(t *testing.T) {
	ownerStore, peerStore := sharedBackendStores(t)
	owner, err := NewEngine(ownerStore, &Options{GenerateID: func() (string, error) { return "shared-modern-input", nil }})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := NewEngine(peerStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	resumed := make(chan TaskInputResponse, 1)
	created, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "shared", AuthContext: "alice",
		Handle: func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
			err := h.RequestInput(ctx, InputRequest{Key: "approval", Method: InputMethodElicitation,
				Payload: json.RawMessage(`{"method":"elicitation/create","params":{"message":"Approve?"}}`)})
			if err != nil {
				return nil, err
			}
			resp, ok, err := h.ModernInputResponse(ctx, "approval")
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("modern input response was not persisted")
			}
			resumed <- resp
			return json.RawMessage(`{"content":[]}`), nil
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, ownerStore, created.ID, protocolcodec.TaskInputRequired)
	waitForModernInputWaiter(t, owner, created.ID, "approval")
	request := ModernRequest{TaskID: created.ID, InputResponses: map[string]TaskInputResponse{
		"approval": {Payload: json.RawMessage(`{"action":"accept"}`)},
	}}
	if _, err := peer.DispatchModern(context.Background(), "mallory", MethodUpdate, request); !errors.Is(err, ErrCrossContext) {
		t.Fatalf("cross-context update error = %v", err)
	}
	if _, err := peer.DispatchModern(context.Background(), "alice", MethodUpdate, request); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-resumed:
		if string(response.Payload) != `{"action":"accept"}` {
			t.Fatalf("modern response = %s", response.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer modern update did not resume owner handler")
	}
	waitForStatus(t, ownerStore, created.ID, protocolcodec.TaskCompleted)
}

func TestSharedBackendLegacyInputReachesOwningEngine(t *testing.T) {
	ownerStore, peerStore := sharedBackendStores(t)
	owner, err := NewEngine(ownerStore, &Options{GenerateID: func() (string, error) { return "shared-legacy-input", nil }})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := NewEngine(peerStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	resumed := make(chan InputResponse, 1)
	created, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "shared", AuthContext: "alice",
		Handle: func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
			resp, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
			if err == nil {
				resumed <- resp
			}
			return json.RawMessage(`{"content":[]}`), err
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, ownerStore, created.ID, protocolcodec.TaskInputRequired)
	waitForPendingLegacyInput(t, owner, created.ID)
	params := mustSupplyInputParamsRaw(t, created.ID, []byte(`{"approved":true}`), false)
	if _, err := peer.DispatchAs(context.Background(), "mallory", MethodSupplyInput, params); !errors.Is(err, ErrCrossContext) {
		t.Fatalf("cross-context supplyInput error = %v", err)
	}
	if _, err := peer.DispatchAs(context.Background(), "alice", MethodSupplyInput, params); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-resumed:
		if string(response.Data) != `{"approved":true}` {
			t.Fatalf("legacy response = %s", response.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer legacy input did not resume owner handler")
	}
	waitForStatus(t, ownerStore, created.ID, protocolcodec.TaskCompleted)
}

type blockingAtomicCreateStore struct {
	TaskStore
	AtomicCreateTaskStore
	AtomicFinalizeTaskStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int64
}

func (s *blockingAtomicCreateStore) CreateWithConcurrencyLimit(ctx context.Context, rec TaskRecord, limit int) error {
	call := s.calls.Add(1)
	if call == 1 {
		s.once.Do(func() { close(s.entered) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.AtomicCreateTaskStore.CreateWithConcurrencyLimit(ctx, rec, limit)
}

func TestConcurrentGeneratedIDCollisionNeverCreatesUnownedTask(t *testing.T) {
	base := NewInMemoryStore()
	store := &blockingAtomicCreateStore{
		TaskStore:               base,
		AtomicCreateTaskStore:   base.(AtomicCreateTaskStore),
		AtomicFinalizeTaskStore: base.(AtomicFinalizeTaskStore),
		entered:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
	newEngine := func() *Engine {
		e, err := NewEngine(store, &Options{GenerateID: func() (string, error) { return "colliding-id", nil }})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	owner, peer := newEngine(), newEngine()
	started := make(chan struct{})
	stopped := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
			ToolName: "first", AuthContext: "alice",
			Run: func(ctx context.Context) (json.RawMessage, error) {
				close(started)
				<-ctx.Done()
				close(stopped)
				return nil, ctx.Err()
			},
		}, true)
		firstDone <- err
	}()
	<-store.entered

	_, collisionErr := peer.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "second", AuthContext: "alice",
		Run: func(context.Context) (json.RawMessage, error) {
			t.Error("colliding task handler ran")
			return json.RawMessage(`{}`), nil
		},
	}, true)
	if collisionErr == nil || !strings.Contains(collisionErr.Error(), "already reserved") {
		t.Fatalf("collision error = %v, want reserved-ID rejection", collisionErr)
	}
	if calls := store.calls.Load(); calls != 1 {
		t.Fatalf("collision reached durable Create: calls = %d, want 1", calls)
	}

	close(store.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := peer.DispatchModern(context.Background(), "alice", MethodCancel, ModernRequest{TaskID: "colliding-id"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("peer cancellation did not clean up the reserved task's worker")
	}
	if taskOwnerFor(store, "colliding-id") != nil {
		t.Fatal("task owner reservation remained after cancellation")
	}
}

func waitForModernInputWaiter(t *testing.T, e *Engine, id, key string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.mu.Lock()
		waiting := len(e.inputWaiters[id][key]) > 0
		e.mu.Unlock()
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("modern input waiter was not registered")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForPendingLegacyInput(t *testing.T, e *Engine, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := e.PendingInput(id); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy elicitation was not registered")
		}
		time.Sleep(time.Millisecond)
	}
}
