package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// legacyStore exposes exactly the pre-atomic-extension TaskStore method set.
// The compile-time assertion prevents optional capabilities from accidentally
// becoming required methods again.
type legacyStore struct{ base tasks.TaskStore }

func (s *legacyStore) Create(ctx context.Context, rec tasks.TaskRecord) error {
	return s.base.Create(ctx, rec)
}
func (s *legacyStore) Get(ctx context.Context, id string) (tasks.TaskRecord, error) {
	return s.base.Get(ctx, id)
}
func (s *legacyStore) Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (tasks.TaskRecord, error) {
	return s.base.Transition(ctx, id, to, msg)
}
func (s *legacyStore) SetResult(ctx context.Context, id string, result tasks.TaskResult) error {
	return s.base.SetResult(ctx, id, result)
}
func (s *legacyStore) AddInputRequest(ctx context.Context, id string, req tasks.InputRequest) error {
	return s.base.AddInputRequest(ctx, id, req)
}
func (s *legacyStore) ApplyInputResponses(ctx context.Context, id string, responses map[string]tasks.TaskInputResponse) (map[string]tasks.TaskInputResponse, tasks.TaskRecord, error) {
	return s.base.ApplyInputResponses(ctx, id, responses)
}
func (s *legacyStore) List(ctx context.Context, cursor string, limit int) ([]tasks.TaskRecord, string, error) {
	return s.base.List(ctx, cursor, limit)
}
func (s *legacyStore) ListByAuthContext(ctx context.Context, authContext, cursor string, limit int) ([]tasks.TaskRecord, string, error) {
	return s.base.ListByAuthContext(ctx, authContext, cursor, limit)
}
func (s *legacyStore) Delete(ctx context.Context, id string) error {
	return s.base.Delete(ctx, id)
}
func (s *legacyStore) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	return s.base.PurgeExpired(ctx, now)
}

var _ tasks.TaskStore = (*legacyStore)(nil)

func TestLegacyStoreFallbackSerializesConcurrencyLimit(t *testing.T) {
	store := &legacyStore{base: tasks.NewInMemoryStore()}
	var ids atomic.Int64
	engine, err := tasks.NewEngine(store, &tasks.Options{
		RequestorIdentifiable: true,
		Lifecycle:             tasks.Lifecycle{MaxConcurrentPerRequestor: 1},
		GenerateID: func() (string, error) {
			return "legacy-" + time.Unix(0, ids.Add(1)).Format("150405.000000000"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	start := make(chan struct{})
	results := make(chan error, 24)
	for range 24 {
		go func() {
			<-start
			_, err := engine.CreateToolTask(context.Background(), tasks.CreateToolCallParams{
				ToolName: "legacy", AuthContext: "alice",
				Run: func(ctx context.Context) (json.RawMessage, error) {
					select {
					case <-release:
					case <-ctx.Done():
					}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			results <- err
		}()
	}
	close(start)
	succeeded := 0
	for range 24 {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("legacy fallback admitted %d tasks, want 1", succeeded)
	}
}

type blockingLegacyStore struct {
	*legacyStore
	listCalls atomic.Int64
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (s *blockingLegacyStore) ListByAuthContext(ctx context.Context, authContext, cursor string, limit int) ([]tasks.TaskRecord, string, error) {
	s.listCalls.Add(1)
	s.once.Do(func() {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
	return s.legacyStore.ListByAuthContext(ctx, authContext, cursor, limit)
}

func TestLegacyStoreFallbackSynchronizesAcrossEngines(t *testing.T) {
	store := &blockingLegacyStore{
		legacyStore: &legacyStore{base: tasks.NewInMemoryStore()},
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	var ids atomic.Int64
	newEngine := func() *tasks.Engine {
		e, err := tasks.NewEngine(store, &tasks.Options{
			RequestorIdentifiable: true,
			Lifecycle:             tasks.Lifecycle{MaxConcurrentPerRequestor: 1},
			GenerateID: func() (string, error) {
				return fmt.Sprintf("cross-engine-%d", ids.Add(1)), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	engines := []*tasks.Engine{newEngine(), newEngine()}
	releaseWork := make(chan struct{})
	defer close(releaseWork)
	results := make(chan error, 2)
	create := func(e *tasks.Engine) {
		_, err := e.CreateToolTask(context.Background(), tasks.CreateToolCallParams{
			ToolName: "legacy", AuthContext: "alice",
			Run: func(context.Context) (json.RawMessage, error) {
				<-releaseWork
				return json.RawMessage(`{}`), nil
			},
		}, true)
		results <- err
	}
	go create(engines[0])
	<-store.entered
	go create(engines[1])
	time.Sleep(20 * time.Millisecond)
	if got := store.listCalls.Load(); got != 1 {
		t.Fatalf("second engine entered fallback critical section early: %d list calls", got)
	}
	close(store.release)
	err1, err2 := <-results, <-results
	if (err1 == nil) == (err2 == nil) ||
		err1 != nil && !errors.Is(err1, tasks.ErrConcurrencyCap) ||
		err2 != nil && !errors.Is(err2, tasks.ErrConcurrencyCap) {
		t.Fatalf("cross-engine admission errors = %v, %v; want one success and one cap", err1, err2)
	}
}
