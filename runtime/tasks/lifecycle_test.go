package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
	storeinmem "github.com/hurtener/dockyard/runtime/store/inmem"
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

	// Seed an already-expired terminal task and a live one directly.
	expired := TaskRecord{
		ID: "expired", Status: protocolcodec.TaskWorking,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(-time.Minute), Method: "tools/call",
	}
	if err := store.Create(context.Background(), expired); err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	if _, _, err := store.(AtomicFinalizeTaskStore).Finalize(context.Background(), expired.ID, protocolcodec.TaskCompleted, "done", TaskResult{Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Finalize expired: %v", err)
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
	s1.Start(context.Background()) // stopped sweeps cannot be restarted

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

type blockingPurgeStore struct {
	TaskStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingPurgeStore) PurgeExpired(context.Context, time.Time) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return 0, nil
}

func TestPurgeSweep_ConcurrentStopCallersJoinShutdown(t *testing.T) {
	store := &blockingPurgeStore{
		TaskStore: NewInMemoryStore(),
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	sweep := newPurgeSweep(store, time.Millisecond, quietLogger())
	sweep.Start(context.Background())
	<-store.entered

	returned := make(chan int, 2)
	go func() {
		sweep.Stop()
		returned <- 1
	}()

	// Ensure the first caller initiated shutdown before the second enters Stop.
	for {
		sweep.mu.Lock()
		stopped := sweep.stopped
		sweep.mu.Unlock()
		if stopped {
			break
		}
		time.Sleep(time.Millisecond)
	}
	go func() {
		sweep.Stop()
		returned <- 2
	}()

	select {
	case caller := <-returned:
		t.Fatalf("Stop caller %d returned while PurgeExpired was blocked", caller)
	case <-time.After(50 * time.Millisecond):
	}

	close(store.release)
	for range 2 {
		select {
		case <-returned:
		case <-time.After(2 * time.Second):
			t.Fatal("Stop caller did not return after PurgeExpired completed")
		}
	}
}

func TestEnginePurgeExpiresActiveInputTaskAndReleasesCap(t *testing.T) {
	store := NewInMemoryStore()
	e, err := NewEngine(store, &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
		Lifecycle:             Lifecycle{MaxConcurrentPerRequestor: 1},
		GenerateID: func() (string, error) {
			return "expired-active", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inputDone := make(chan error, 1)
	ttl := int64(1)
	created, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "input", AuthContext: "alice", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Handle: func(_ context.Context, h TaskHandle) (json.RawMessage, error) {
			err := h.RequestInput(context.Background(), InputRequest{
				Key: "roots", Method: InputMethodRoots, Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
			})
			inputDone <- err
			return nil, err
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, created.ID, protocolcodec.TaskInputRequired)
	if _, err := e.purgeExpired(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expired active task still stored: %v", err)
	}
	select {
	case err := <-inputDone:
		if err == nil {
			t.Fatal("expired input waiter resumed successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expired task did not cancel its background-context input waiter")
	}
	e.life.MaxConcurrentPerRequestor = 1
	e.genID = func() (string, error) { return "after-expiry", nil }
	release := make(chan struct{})
	defer close(release)
	if _, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{
		ToolName: "next", AuthContext: "alice", Run: blockingRun(release, []byte(`{}`), nil),
	}); err != nil {
		t.Fatalf("expired task retained concurrency slot: %v", err)
	}
}

func TestEnginePurgeSnapshotCannotStarveUnderSustainedCreation(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Now().UTC()
	const expiredCount = 500
	for i := range expiredCount {
		rec := workingRecord(fmt.Sprintf("expired-%04d", i))
		rec.ExpiresAt = now.Add(-time.Minute)
		if err := store.Create(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	e, err := NewEngine(store, &Options{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	created := make(chan struct{})
	go func() {
		defer close(created)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_ = store.Create(context.Background(), workingRecord(fmt.Sprintf("live-%08d", i)))
			}
		}
	}()
	if _, err := e.purgeExpired(context.Background(), now); err != nil {
		close(stop)
		<-created
		t.Fatal(err)
	}
	close(stop)
	<-created
	for i := range expiredCount {
		if _, err := store.Get(context.Background(), fmt.Sprintf("expired-%04d", i)); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("expired task %d survived snapshot purge: %v", i, err)
		}
	}
}

func TestEnginePurgeEmitsExpirationTerminalEventExactlyOnce(t *testing.T) {
	store := NewInMemoryStore()
	emitter := &captureEmitter{}
	e, err := NewEngine(store, &Options{Logger: quietLogger(), Obs: emitter, GenerateID: func() (string, error) {
		return "expires-once", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ttl := int64(1)
	started := make(chan struct{})
	if _, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "expire", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Run: func(ctx context.Context) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	<-started
	now := time.Now().Add(time.Hour)
	errCh := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.purgeExpired(context.Background(), now)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	ends := emitter.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)
	if len(ends) != 1 {
		t.Fatalf("expiration terminal events = %d, want 1", len(ends))
	}
	var payload obs.TaskProgressPayload
	if err := json.Unmarshal(ends[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TaskID != "expires-once" || payload.Status != string(protocolcodec.TaskCancelled) || payload.Message != "The task expired." {
		t.Fatalf("expiration payload = %#v", payload)
	}
}

func TestNonOwnerPurgeRoutesExpiredTaskCleanupToOwner(t *testing.T) {
	store := NewInMemoryStore()
	ownerEvents := &captureEmitter{}
	sweeperEvents := &captureEmitter{}
	owner, err := NewEngine(store, &Options{
		Logger: quietLogger(), Obs: ownerEvents, ServerID: "owner",
		GenerateID: func() (string, error) { return "shared-expired", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	sweeper, err := NewEngine(store, &Options{Logger: quietLogger(), Obs: sweeperEvents, ServerID: "sweeper"})
	if err != nil {
		t.Fatal(err)
	}

	handlerDone := make(chan error, 1)
	ttl := int64(1)
	created, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "shared", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Handle: func(_ context.Context, h TaskHandle) (json.RawMessage, error) {
			err := h.RequestInput(context.Background(), InputRequest{
				Key: "roots", Method: InputMethodRoots,
				Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
			})
			handlerDone <- err
			return nil, err
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, created.ID, protocolcodec.TaskInputRequired)

	resultDone := make(chan error, 1)
	resultParams := mustTaskIDParams(t, created.ID)
	go func() {
		_, err := owner.Dispatch(context.Background(), MethodResult, resultParams)
		resultDone <- err
	}()
	waitForEngineWaiter(t, owner, created.ID)

	if _, err := sweeper.purgeExpired(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	localStateRemaining := len(owner.waiters[created.ID]) + len(owner.inputWaiters[created.ID])
	if owner.cancels[created.ID] != nil {
		localStateRemaining++
	}
	if _, ok := owner.spans[created.ID]; ok {
		localStateRemaining++
	}
	owner.mu.Unlock()
	if localStateRemaining != 0 {
		t.Fatalf("non-owner purge returned before owner cleanup; remaining entries = %d", localStateRemaining)
	}
	select {
	case err := <-handlerDone:
		if err == nil {
			t.Fatal("expired input waiter resumed successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("non-owner sweep did not cancel the owner's input waiter")
	}
	select {
	case <-resultDone:
	case <-time.After(2 * time.Second):
		t.Fatal("non-owner sweep did not wake the owner's result waiter")
	}

	starts := ownerEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseStart)
	ends := ownerEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)
	if len(starts) != 1 || len(ends) != 1 || starts[0].TraceID != ends[0].TraceID {
		t.Fatalf("owner lifecycle events = starts %#v, ends %#v", starts, ends)
	}
	if got := sweeperEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd); len(got) != 0 {
		t.Fatalf("non-owner emitted terminal events: %#v", got)
	}
	if taskOwnerFor(store, created.ID) != nil {
		t.Fatal("terminal task retained an ownership registry entry")
	}
	if _, err := store.Get(context.Background(), created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expired task still stored: %v", err)
	}
}

func TestDurableStoreWrappersShareTaskOwnerForExpiry(t *testing.T) {
	backing := storeinmem.New()
	t.Cleanup(func() { _ = backing.Close() })
	if err := backing.Migrate(context.Background(), Migrations()); err != nil {
		t.Fatal(err)
	}
	ownerStore, err := NewStore(backing)
	if err != nil {
		t.Fatal(err)
	}
	sweeperStore, err := NewStore(backing)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewEngine(ownerStore, &Options{
		Logger: quietLogger(), GenerateID: func() (string, error) { return "wrapped-shared", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	sweeper, err := NewEngine(sweeperStore, &Options{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	cancelled := make(chan struct{})
	ttl := int64(1)
	if _, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "shared", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Run: func(ctx context.Context) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := sweeper.purgeExpired(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry through a second durable wrapper did not cancel the owning worker")
	}
	if _, err := ownerStore.Get(context.Background(), "wrapped-shared"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expired task remains stored: %v", err)
	}
	if taskOwnerFor(ownerStore, "wrapped-shared") != nil || taskOwnerFor(sweeperStore, "wrapped-shared") != nil {
		t.Fatal("shared durable wrappers retained an ownership registry entry")
	}
}

func TestSharedStoreConcurrentExpiryHasOneOwnerCleanup(t *testing.T) {
	store := NewInMemoryStore()
	ownerEvents := &captureEmitter{}
	otherEvents := &captureEmitter{}
	owner, err := NewEngine(store, &Options{
		Logger: quietLogger(), Obs: ownerEvents,
		GenerateID: func() (string, error) { return "shared-race", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewEngine(store, &Options{Logger: quietLogger(), Obs: otherEvents})
	if err != nil {
		t.Fatal(err)
	}
	ttl := int64(1)
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	if _, err := owner.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "race", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Run: func(ctx context.Context) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(handlerDone)
			return nil, ctx.Err()
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	<-started

	now := time.Now().Add(time.Hour)
	startSweep := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		engine := owner
		if i%2 == 1 {
			engine = other
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startSweep
			_, err := engine.purgeExpired(context.Background(), now)
			errs <- err
		}()
	}
	close(startSweep)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shared-store expiry did not cancel the owning handler")
	}
	if got := len(ownerEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)); got != 1 {
		t.Fatalf("owner terminal event count = %d, want 1", got)
	}
	if got := len(otherEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)); got != 0 {
		t.Fatalf("non-owner terminal event count = %d, want 0", got)
	}
	if taskOwnerFor(store, "shared-race") != nil {
		t.Fatal("raced expiry retained an ownership registry entry")
	}
}

func TestExpiredOrphanWithoutLocalOwnerIsSafelyReaped(t *testing.T) {
	store := NewInMemoryStore()
	rec := workingRecord("orphan")
	rec.ExpiresAt = time.Now().Add(-time.Minute)
	if err := store.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	emitter := &captureEmitter{}
	sweeper, err := NewEngine(store, &Options{Logger: quietLogger(), Obs: emitter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.purgeExpired(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), rec.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("orphan was not reaped: %v", err)
	}
	if got := len(emitter.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)); got != 1 {
		t.Fatalf("orphan terminal event count = %d, want 1", got)
	}
}

type blockedAtomicCreateStore struct {
	TaskStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockedAtomicCreateStore) CreateWithConcurrencyLimit(ctx context.Context, rec TaskRecord, limit int) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.TaskStore.(AtomicCreateTaskStore).CreateWithConcurrencyLimit(ctx, rec, limit)
}

func TestExpiryWaitsForOwnershipRegistrationBeforeReapingOrphan(t *testing.T) {
	base := NewInMemoryStore()
	orphan := workingRecord("creation-race")
	orphan.ExpiresAt = time.Now().Add(-time.Minute)
	if err := base.Create(context.Background(), orphan); err != nil {
		t.Fatal(err)
	}
	store := &blockedAtomicCreateStore{
		TaskStore: base, entered: make(chan struct{}), release: make(chan struct{}),
	}
	creatorEvents := &captureEmitter{}
	sweeperEvents := &captureEmitter{}
	creator, err := NewEngine(store, &Options{
		Logger: quietLogger(), Obs: creatorEvents,
		GenerateID: func() (string, error) { return orphan.ID, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	sweeper, err := NewEngine(store, &Options{Logger: quietLogger(), Obs: sweeperEvents})
	if err != nil {
		t.Fatal(err)
	}
	createDone := make(chan error, 1)
	go func() {
		_, err := creator.CreateToolTask(context.Background(), CreateToolCallParams{
			ToolName: "duplicate", Run: instantRun(json.RawMessage(`{}`), nil),
		}, true)
		createDone <- err
	}()
	<-store.entered

	purgeCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := sweeper.purgeExpired(purgeCtx, time.Now()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("purge passed an unresolved ownership reservation: %v", err)
	}
	if _, err := base.Get(context.Background(), orphan.ID); err != nil {
		t.Fatalf("purge deleted orphan before ownership resolved: %v", err)
	}
	close(store.release)
	if err := <-createDone; err == nil {
		t.Fatal("duplicate creation unexpectedly succeeded")
	}
	if _, err := sweeper.purgeExpired(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Get(context.Background(), orphan.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("orphan was not reaped after failed creation: %v", err)
	}
	if got := len(creatorEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)); got != 0 {
		t.Fatalf("failed creator emitted orphan terminal event: %d", got)
	}
	if got := len(sweeperEvents.byKindPhase(obs.KindTaskProgress, obs.PhaseEnd)); got != 1 {
		t.Fatalf("sweeper orphan terminal event count = %d, want 1", got)
	}
}

func TestTaskOwnershipRegistryUnregistersOnTerminalAndAdmissionDelete(t *testing.T) {
	store := NewInMemoryStore()
	ids := []string{"completed-owner", "deleted-owner"}
	next := 0
	e, err := NewEngine(store, &Options{
		Logger: quietLogger(),
		GenerateID: func() (string, error) {
			id := ids[next]
			next++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "complete", Run: instantRun(json.RawMessage(`{}`), nil),
	}, true); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, "completed-owner", protocolcodec.TaskCompleted)
	waitForOwnerUnregistered(t, store, "completed-owner")

	ctx, admission := WithDeferredAdmission(context.Background())
	if _, err := e.CreateToolTask(ctx, CreateToolCallParams{
		ToolName: "delete", Run: instantRun(json.RawMessage(`{}`), nil),
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := admission.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "deleted-owner"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("aborted task was not deleted: %v", err)
	}
	waitForOwnerUnregistered(t, store, "deleted-owner")
}

func waitForEngineWaiter(t *testing.T, e *Engine, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.mu.Lock()
		waiting := len(e.waiters[id]) > 0
		e.mu.Unlock()
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("result waiter was not registered")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForOwnerUnregistered(t *testing.T, store TaskStore, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for taskOwnerFor(store, id) != nil {
		if time.Now().After(deadline) {
			t.Fatalf("task %q retained an ownership registry entry", id)
		}
		time.Sleep(time.Millisecond)
	}
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
