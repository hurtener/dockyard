package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

type deadlineFinalizeStore struct {
	TaskStore
	calls atomic.Int32
}

func (s *deadlineFinalizeStore) Finalize(
	ctx context.Context, _ string, _ protocolcodec.TaskStatus, _ string, _ TaskResult,
) (TaskRecord, bool, error) {
	s.calls.Add(1)
	<-ctx.Done()
	return TaskRecord{}, false, ctx.Err()
}

type failDeleteOnceStore struct {
	TaskStore
	failed atomic.Bool
}

type blockingOneDeleteStore struct {
	TaskStore
	blocked string
}

func (s *blockingOneDeleteStore) Delete(ctx context.Context, id string) error {
	if id == s.blocked {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.TaskStore.Delete(ctx, id)
}

func (s *failDeleteOnceStore) Delete(ctx context.Context, id string) error {
	if s.failed.CompareAndSwap(false, true) {
		return errors.New("transient delete failure")
	}
	return s.TaskStore.Delete(ctx, id)
}

func TestDeferredAdmissionDeleteFailureTerminalizesTask(t *testing.T) {
	store := &failDeleteOnceStore{TaskStore: NewInMemoryStore()}
	var ids atomic.Int64
	e, err := NewEngine(store, &Options{
		RequestorIdentifiable: true,
		Lifecycle:             Lifecycle{MaxConcurrentPerRequestor: 1},
		GenerateID: func() (string, error) {
			if ids.Add(1) == 1 {
				return "aborted", nil
			}
			return "replacement", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, admission := WithDeferredAdmission(context.Background())
	started := atomic.Bool{}
	if _, err := e.CreateToolTask(ctx, CreateToolCallParams{
		ToolName: "deferred", AuthContext: "alice",
		Run: func(context.Context) (json.RawMessage, error) {
			started.Store(true)
			return json.RawMessage(`{}`), nil
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := admission.Abort(context.Background()); err == nil {
		t.Fatal("abort unexpectedly hid delete failure")
	}
	rec, err := store.Get(context.Background(), "aborted")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != protocolcodec.TaskCancelled || started.Load() {
		t.Fatalf("aborted task = %#v, started = %v", rec, started.Load())
	}
	release := make(chan struct{})
	defer close(release)
	if _, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "replacement", AuthContext: "alice",
		Run: func(context.Context) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`{}`), nil
		},
	}, true); err != nil {
		t.Fatalf("terminalized aborted task retained cap slot: %v", err)
	}
}

func TestDeferredAdmissionRejectsTaskChangedBeforeStart(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"deleted", "cancelled", "expired"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryStore()
			e, err := NewEngine(store, &Options{GenerateID: func() (string, error) { return state, nil }})
			if err != nil {
				t.Fatal(err)
			}
			ctx, admission := WithDeferredAdmission(context.Background())
			started := atomic.Bool{}
			if _, err := e.CreateToolTask(ctx, CreateToolCallParams{
				ToolName: state,
				Run: func(context.Context) (json.RawMessage, error) {
					started.Store(true)
					return json.RawMessage(`{}`), nil
				},
			}, true); err != nil {
				t.Fatal(err)
			}

			switch state {
			case "deleted":
				if err := store.Delete(context.Background(), state); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				atomicStore := store.(AtomicFinalizeTaskStore)
				if _, _, err := atomicStore.Finalize(context.Background(), state, protocolcodec.TaskCancelled,
					"cancelled before admission", TaskResult{Err: "cancelled"}); err != nil {
					t.Fatal(err)
				}
			case "expired":
				mem := store.(*inMemoryStore)
				mem.mu.Lock()
				rec := mem.tasks[state]
				rec.ExpiresAt = time.Now().Add(-time.Second)
				mem.tasks[state] = rec
				mem.mu.Unlock()
			}

			_, admitted, err := admission.AdmitCanonical(context.Background(), state)
			if !admitted || err == nil {
				t.Fatalf("AdmitCanonical admitted=%v error=%v, want selected task rejected", admitted, err)
			}
			if started.Load() {
				t.Fatal("handler started after durable task became unavailable")
			}
		})
	}
}

func TestDeferredAdmissionSharesOneCleanupDeadline(t *testing.T) {
	t.Parallel()
	store := &deadlineFinalizeStore{TaskStore: NewInMemoryStore()}
	var next atomic.Int32
	e, err := NewEngine(store, &Options{GenerateID: func() (string, error) {
		if next.Add(1) == 1 {
			return "first", nil
		}
		return "second", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, admission := WithDeferredAdmission(context.Background())
	for range 2 {
		if _, err := e.CreateToolTask(ctx, CreateToolCallParams{
			ToolName: "deferred", Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
		}, true); err != nil {
			t.Fatal(err)
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := admission.Abort(cleanupCtx); err != nil {
		t.Fatalf("Abort returned an error after Delete cleaned every task: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("two aborts reset the shared cleanup deadline: %v", elapsed)
	}
	if calls := store.calls.Load(); calls != 2 {
		t.Fatalf("Finalize calls = %d, want both tasks attempted within one deadline", calls)
	}
	recs, _, err := store.List(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("concurrent cleanup left records: %#v", recs)
	}
}

func TestDeferredAdmissionAbortDoesNotOrphanTasksBehindOneBlocker(t *testing.T) {
	base := NewInMemoryStore()
	store := &blockingOneDeleteStore{TaskStore: base, blocked: "blocked"}
	var next atomic.Int32
	e, err := NewEngine(store, &Options{GenerateID: func() (string, error) {
		ids := []string{"blocked", "clean-one", "clean-two"}
		return ids[next.Add(1)-1], nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, admission := WithDeferredAdmission(context.Background())
	for range 3 {
		if _, err := e.CreateToolTask(ctx, CreateToolCallParams{
			ToolName: "deferred", Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
		}, true); err != nil {
			t.Fatal(err)
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if err := admission.Abort(cleanupCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Abort error = %v, want blocker deadline", err)
	}
	for _, id := range []string{"clean-one", "clean-two"} {
		if _, err := base.Get(context.Background(), id); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("nonblocking task %q was orphaned: %v", id, err)
		}
	}
	rec, err := base.Get(context.Background(), "blocked")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != protocolcodec.TaskCancelled {
		t.Fatalf("blocked cleanup retained active task: %#v", rec)
	}
}
