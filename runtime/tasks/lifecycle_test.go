package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestLifecycle_EnforcedTTL is the TTL-clamping table: a requested TTL above
// the manifest max is clamped down; an absent request gets the default; with no
// default and no request the task is unlimited (RFC §8.5).
func TestLifecycle_EnforcedTTL(t *testing.T) {
	t.Parallel()
	ptr := func(v int64) *int64 { return &v }
	cases := []struct {
		name      string
		life      Lifecycle
		requested *int64
		want      *int64 // nil means unlimited
	}{
		{"no-limits-no-request", Lifecycle{}, nil, nil},
		{"default-applied", Lifecycle{DefaultTTL: time.Minute}, nil, ptr(60000)},
		{"request-honoured", Lifecycle{MaxTTL: time.Hour}, ptr(60000), ptr(60000)},
		{"request-clamped", Lifecycle{MaxTTL: time.Minute}, ptr(3600000), ptr(60000)},
		{"default-clamped", Lifecycle{MaxTTL: time.Minute, DefaultTTL: time.Hour}, nil, ptr(60000)},
		{"zero-request-uses-default", Lifecycle{DefaultTTL: time.Minute}, ptr(0), ptr(60000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.life.enforcedTTL(tc.requested)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("want unlimited TTL, got %d", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("want %d, got unlimited", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Fatalf("enforced TTL = %d, want %d", *got, *tc.want)
			}
		})
	}
}

// TestCreateForToolCall_StampsEnforcedTTL proves CreateForToolCall records the
// clamped TTL and an ExpiresAt on the durable record.
func TestCreateForToolCall_StampsEnforcedTTL(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, err := NewEngine(store, &Options{
		Logger:    quietLogger(),
		Lifecycle: Lifecycle{MaxTTL: time.Minute},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	requested := int64(3600000) // 1h — above the 1m max
	raw, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{
		ToolName: "x",
		TaskMeta: protocolcodec.TaskMeta{TTL: &requested},
		Run:      instantRun([]byte(`{}`), nil),
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec, err := store.Get(context.Background(), res.Task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.TTL == nil || *rec.TTL != 60000 {
		t.Fatalf("enforced TTL not stamped: %v", rec.TTL)
	}
	if rec.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt not stamped for a TTL-bounded task")
	}
	if res.Task.TTL == nil || *res.Task.TTL != 60000 {
		t.Fatalf("CreateTaskResult reports requested, not enforced, TTL: %v", res.Task.TTL)
	}
}

// TestConcurrencyCap_RejectsOverCap proves the per-requestor concurrent-task
// cap rejects a create that would exceed it (RFC §8.5; brief 02 §4.6).
func TestConcurrencyCap_RejectsOverCap(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
		Lifecycle:             Lifecycle{MaxConcurrentPerRequestor: 2},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	release := make(chan struct{})
	defer close(release)
	ctx := context.Background()
	mk := func() error {
		_, err := e.CreateForToolCall(ctx, CreateToolCallParams{
			ToolName:    "x",
			AuthContext: "alice",
			Run:         blockingRun(release, []byte(`{}`), nil),
		})
		return err
	}
	if err := mk(); err != nil {
		t.Fatalf("task 1: %v", err)
	}
	if err := mk(); err != nil {
		t.Fatalf("task 2: %v", err)
	}
	// The third task pushes alice over the cap of 2.
	if err := mk(); err == nil {
		t.Fatal("task 3 must be rejected — alice is at the cap")
	} else if !errors.Is(err, ErrConcurrencyCap) {
		t.Fatalf("want ErrConcurrencyCap, got %v", err)
	}
	// A different requestor is unaffected.
	if _, err := e.CreateForToolCall(ctx, CreateToolCallParams{
		ToolName: "x", AuthContext: "bob", Run: blockingRun(release, []byte(`{}`), nil),
	}); err != nil {
		t.Fatalf("bob's first task must be allowed: %v", err)
	}
}

// TestConcurrencyCap_TerminalTasksDoNotCount proves a terminal task releases
// its slot against the cap.
func TestConcurrencyCap_TerminalTasksDoNotCount(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
		Lifecycle:             Lifecycle{MaxConcurrentPerRequestor: 1},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	// First task completes instantly.
	id := mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	if _, err := e.Dispatch(ctx, MethodResult, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("await terminal: %v", err)
	}
	// The cap is 1, but the first task is terminal — a second create succeeds.
	if _, err := e.CreateForToolCall(ctx, CreateToolCallParams{
		ToolName: "x", AuthContext: "alice", Run: instantRun([]byte(`{}`), nil),
	}); err != nil {
		t.Fatalf("second task after the first went terminal: %v", err)
	}
}

// TestPurgeSweep_ReapsExpiredTasks proves the background TTL purge sweep reaps
// expired tasks and shuts down cleanly (RFC §8.5).
func TestPurgeSweep_ReapsExpiredTasks(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	now := time.Now().UTC()

	// Seed an already-expired task and a live one directly.
	expired := TaskRecord{
		ID: "expired", Status: protocolcodec.TaskWorking,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(-time.Minute), Method: "tools/call",
	}
	if err := store.Create(context.Background(), expired); err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	live := TaskRecord{
		ID: "live", Status: protocolcodec.TaskWorking,
		CreatedAt: now, UpdatedAt: now,
		ExpiresAt: now.Add(time.Hour), Method: "tools/call",
	}
	if err := store.Create(context.Background(), live); err != nil {
		t.Fatalf("Create live: %v", err)
	}

	sweep := newPurgeSweep(store, 5*time.Millisecond, quietLogger())
	if sweep == nil {
		t.Fatal("newPurgeSweep returned nil for a positive interval")
	}
	sweep.Start(context.Background())

	// Wait for the sweep to reap the expired task.
	deadline := time.After(2 * time.Second)
	for {
		_, err := store.Get(context.Background(), "expired")
		if errors.Is(err, ErrTaskNotFound) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("purge sweep did not reap the expired task")
		case <-time.After(5 * time.Millisecond):
		}
	}
	sweep.Stop()

	// The live task survives.
	if _, err := store.Get(context.Background(), "live"); err != nil {
		t.Fatalf("purge sweep reaped a live task: %v", err)
	}
}

// TestPurgeSweep_StopIsCleanAndIdempotent proves Stop joins the goroutine and
// is safe to call repeatedly, including before Start.
func TestPurgeSweep_StopIsCleanAndIdempotent(t *testing.T) {
	t.Parallel()
	// Stop before Start.
	s1 := newPurgeSweep(NewInMemoryStore(), time.Millisecond, quietLogger())
	s1.Stop()
	s1.Stop()

	// Stop after Start, twice.
	s2 := newPurgeSweep(NewInMemoryStore(), time.Millisecond, quietLogger())
	s2.Start(context.Background())
	s2.Stop()
	s2.Stop()

	// A nil sweep (no interval configured) — Start/Stop are no-ops.
	var nilSweep *purgeSweep
	nilSweep.Start(context.Background())
	nilSweep.Stop()
}

// TestPurgeSweep_HonoursContextCancellation proves the sweep goroutine exits
// when its context is cancelled.
func TestPurgeSweep_HonoursContextCancellation(t *testing.T) {
	t.Parallel()
	sweep := newPurgeSweep(NewInMemoryStore(), time.Millisecond, quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	sweep.Start(ctx)
	cancel()
	// done must close once the goroutine observes the cancelled context.
	select {
	case <-sweep.done:
	case <-time.After(2 * time.Second):
		t.Fatal("purge sweep did not stop on context cancellation")
	}
	sweep.Stop() // idempotent after the goroutine already exited
}

// TestEngineSweep_ConcurrentWithLiveTasks proves the engine's purge sweep runs
// safely while tasks are being created and driven (a reusable concurrent
// artifact under -race).
func TestEngineSweep_ConcurrentWithLiveTasks(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:    quietLogger(),
		Lifecycle: Lifecycle{PurgeInterval: time.Millisecond, MaxTTL: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.StartSweep(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ttl := int64(5) // 5ms — the sweep will race these
				_, _ = e.CreateForToolCall(ctx, CreateToolCallParams{
					ToolName: "x",
					TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
					Run:      instantRun([]byte(`{}`), nil),
				})
			}
		}()
	}
	wg.Wait()
	cancel()
	e.StopSweep()
}

// mustCreateAuth creates a task under authContext and returns its ID.
func mustCreateAuth(t *testing.T, e *Engine, authContext string, run RunFunc) string {
	t.Helper()
	raw, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{
		ToolName: "x", AuthContext: authContext, Run: run,
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return res.Task.ID
}

// mustTaskIDParams encodes a tasks/* taskId params object.
func mustTaskIDParams(t *testing.T, id string) []byte {
	t.Helper()
	p, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).
		EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
	if err != nil {
		t.Fatalf("EncodeTaskIDParams: %v", err)
	}
	return p
}
