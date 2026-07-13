package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestEngineConcurrentReuse proves the Engine is safe for concurrent use — the
// "reusable artifact ⇒ concurrent-reuse test under -race" rule (AGENTS.md §14).
// Many goroutines create tasks and dispatch tasks/* against the one engine.
func TestEngineConcurrentReuse(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{Logger: quietLogger(), AdvertiseList: true, RequestorIdentifiable: true})
	codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)

	const workers = 24
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			ctx := context.Background()
			raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{
				ToolName: "concurrent",
				Run:      instantRun(json.RawMessage(`{"isError":false}`), nil),
			})
			if err != nil {
				t.Errorf("CreateForToolCall: %v", err)
				return
			}
			res, err := codec.DecodeCreateTaskResult(raw)
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			id := res.Task.ID

			// Concurrent tasks/get on the same engine.
			p, _ := codec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
			if _, err := e.Dispatch(ctx, MethodGet, p); err != nil {
				t.Errorf("tasks/get: %v", err)
			}
			// tasks/result blocks until terminal — the task completes instantly.
			if _, err := e.Dispatch(ctx, MethodResult, p); err != nil {
				t.Errorf("tasks/result: %v", err)
			}
			// tasks/list races other goroutines' creates.
			if _, err := e.Dispatch(ctx, MethodList, nil); err != nil {
				t.Errorf("tasks/list: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestEngineConcurrentResultWaiters proves many goroutines may block on
// tasks/result for the same task and all wake when it finishes.
func TestEngineConcurrentResultWaiters(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{Logger: quietLogger()})
	codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)
	release := make(chan struct{})
	id := taskIDOf(t, e, blockingRun(release, json.RawMessage(`{"isError":false}`), nil))
	p, _ := codec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})

	const waiters = 16
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			if _, err := e.Dispatch(context.Background(), MethodResult, p); err != nil {
				t.Errorf("tasks/result: %v", err)
			}
		}()
	}
	close(release)
	wg.Wait()
}

func TestCancelledResultWaitUnregistersWaiter(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{Logger: quietLogger()})
	release := make(chan struct{})
	defer close(release)
	id := taskIDOf(t, e, blockingRun(release, json.RawMessage(`{}`), nil))
	params := mustTaskIDParams(t, id)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.Dispatch(ctx, MethodResult, params)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		e.mu.Lock()
		registered := len(e.waiters[id]) == 1
		e.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tasks/result waiter was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled tasks/result error = %v", err)
	}
	e.mu.Lock()
	remaining := len(e.waiters[id])
	e.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("cancelled tasks/result retained %d waiters", remaining)
	}
}

type blockingSecondGetStore struct {
	TaskStore
	mu      sync.Mutex
	gets    int
	entered chan struct{}
	release chan struct{}
}

func (s *blockingSecondGetStore) Get(ctx context.Context, id string) (TaskRecord, error) {
	s.mu.Lock()
	s.gets++
	second := s.gets == 2
	s.mu.Unlock()
	if second {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return TaskRecord{}, ctx.Err()
		}
	}
	return s.TaskStore.Get(ctx, id)
}

func TestTerminalResultRegistrationRaceUnregistersWaiter(t *testing.T) {
	base := NewInMemoryStore()
	store := &blockingSecondGetStore{TaskStore: base, entered: make(chan struct{}), release: make(chan struct{})}
	e, err := NewEngine(store, &Options{Logger: quietLogger(), GenerateID: func() (string, error) { return "result-race", nil }})
	if err != nil {
		t.Fatal(err)
	}
	work := make(chan struct{})
	id := taskIDOf(t, e, blockingRun(work, json.RawMessage(`{}`), nil))
	done := make(chan error, 1)
	go func() {
		_, err := e.Dispatch(context.Background(), MethodResult, mustTaskIDParams(t, id))
		done <- err
	}()
	<-store.entered
	close(work)
	waitForStatus(t, base, id, protocolcodec.TaskCompleted)
	close(store.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	remaining := len(e.waiters[id])
	e.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("terminal registration race retained %d waiters", remaining)
	}
}
