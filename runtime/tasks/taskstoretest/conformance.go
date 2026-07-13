// Package taskstoretest holds the shared TaskStore conformance suite (RFC §8.5,
// CLAUDE.md §9). Every TaskStore driver must pass RunConformance — the
// in-memory stub, the durable Store-backed facade over the in-memory Store, and
// the durable facade over the modernc.org/sqlite Store. A new TaskStore
// guarantee is added here once and proven against every backing, never bolted
// onto one driver (D-070).
//
// A driver's test wires the suite in with a few lines:
//
//	func TestConformance(t *testing.T) {
//		taskstoretest.RunConformance(t, func() tasks.TaskStore { return tasks.NewInMemoryStore() })
//	}
package taskstoretest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// OpenFunc returns a freshly-constructed, empty TaskStore. The suite calls it
// once per case.
type OpenFunc func() tasks.TaskStore

// conformanceCase is one named guarantee in the TaskStore conformance suite.
type conformanceCase struct {
	name string
	fn   func(*testing.T, OpenFunc)
}

// conformanceCases is the full TaskStore conformance suite. It is package-level
// so the harness self-guard can assert the suite is non-empty.
var conformanceCases = []conformanceCase{
	{"CreateGet", testCreateGet},
	{"CreateRejectsNonWorking", testCreateRejectsNonWorking},
	{"CreateRejectsDuplicate", testCreateRejectsDuplicate},
	{"CreateRejectsEmptyID", testCreateRejectsEmptyID},
	{"AtomicConcurrencyLimit", testAtomicConcurrencyLimit},
	{"GetMissing", testGetMissing},
	{"TransitionLifecycle", testTransitionLifecycle},
	{"TransitionSameTerminalIsNoOp", testTransitionSameTerminalNoOp},
	{"TransitionWorkingRefreshesMessage", testTransitionWorkingRefreshesMessage},
	{"SetResultRoundTrips", testSetResult},
	{"FinalizeIsAtomic", testFinalizeAtomic},
	{"InputRequestValidation", testInputRequestValidation},
	{"ListPaginates", testListPaginates},
	{"ListByAuthContextScopes", testListByAuthContextScopes},
	{"DeleteIsIdempotent", testDeleteIdempotent},
	{"ExpiredActiveSnapshot", testExpiredActiveSnapshot},
	{"PurgeExpiredReapsOnlyExpired", testPurgeExpired},
	{"TTLAndExpiryRoundTrip", testTTLRoundTrip},
	{"ScalarPointersAreCloned", testScalarPointersCloned},
	{"CancelledContextsDoNotMutate", testCancelledContexts},
	{"ConcurrentUse", testConcurrentUse},
}

// RunConformance exercises every guarantee of the TaskStore seam against a
// driver. open must return a freshly-constructed, empty TaskStore on each call.
func RunConformance(t *testing.T, open OpenFunc) {
	t.Helper()
	for _, tc := range conformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, open)
		})
	}
}

// Cases returns the number of conformance cases — used by the harness
// self-guard to assert the suite is non-empty.
func Cases() int { return len(conformanceCases) }

func ctx() context.Context { return context.Background() }

func atomicCreate(t *testing.T, s tasks.TaskStore) tasks.AtomicCreateTaskStore {
	t.Helper()
	atomic, ok := s.(tasks.AtomicCreateTaskStore)
	if !ok {
		t.Fatalf("built-in TaskStore %T does not implement AtomicCreateTaskStore", s)
	}
	return atomic
}

func atomicFinalize(t *testing.T, s tasks.TaskStore) tasks.AtomicFinalizeTaskStore {
	t.Helper()
	atomic, ok := s.(tasks.AtomicFinalizeTaskStore)
	if !ok {
		t.Fatalf("built-in TaskStore %T does not implement AtomicFinalizeTaskStore", s)
	}
	return atomic
}

func expiredActive(t *testing.T, s tasks.TaskStore) tasks.ExpiredActiveTaskStore {
	t.Helper()
	scanner, ok := s.(tasks.ExpiredActiveTaskStore)
	if !ok {
		t.Fatalf("built-in TaskStore %T does not implement ExpiredActiveTaskStore", s)
	}
	return scanner
}

func working(id string) tasks.TaskRecord {
	now := time.Now().UTC()
	return tasks.TaskRecord{
		ID:        id,
		Status:    protocolcodec.TaskWorking,
		CreatedAt: now,
		UpdatedAt: now,
		Method:    "tools/call",
	}
}

func testCreateGet(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx(), "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "t1" || got.Status != protocolcodec.TaskWorking {
		t.Fatalf("unexpected record: %#v", got)
	}
}

func testCreateRejectsNonWorking(t *testing.T, open OpenFunc) {
	s := open()
	rec := working("t1")
	rec.Status = protocolcodec.TaskCompleted
	if err := s.Create(ctx(), rec); err == nil {
		t.Fatal("Create must reject a task that does not begin in working")
	}
}

func testCreateRejectsDuplicate(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("dup")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := s.Create(ctx(), working("dup")); err == nil {
		t.Fatal("Create must reject a duplicate task ID")
	}
}

func testCreateRejectsEmptyID(t *testing.T, open OpenFunc) {
	s := open()
	rec := working("")
	if err := s.Create(ctx(), rec); err == nil {
		t.Fatal("Create must reject an empty task ID")
	}
}

func testAtomicConcurrencyLimit(t *testing.T, open OpenFunc) {
	s := open()
	atomic := atomicCreate(t, s)
	const callers = 24
	start := make(chan struct{})
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rec := working(fmt.Sprintf("limited-%02d", i))
			rec.AuthContext = "alice"
			results <- atomic.CreateWithConcurrencyLimit(ctx(), rec, 1)
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, tasks.ErrConcurrencyCap):
		default:
			t.Fatalf("unexpected admission error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("atomic cap admitted %d tasks, want exactly 1", succeeded)
	}
}

func testGetMissing(t *testing.T, open OpenFunc) {
	s := open()
	_, err := s.Get(ctx(), "nope")
	if !errors.Is(err, tasks.ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
}

// testTransitionLifecycle is the binding lifecycle-transition table: every
// legal transition succeeds, every illegal one is a typed error.
func testTransitionLifecycle(t *testing.T, open OpenFunc) {
	all := []protocolcodec.TaskStatus{
		protocolcodec.TaskWorking,
		protocolcodec.TaskInputRequired,
		protocolcodec.TaskCompleted,
		protocolcodec.TaskFailed,
		protocolcodec.TaskCancelled,
	}
	for _, from := range all {
		for _, to := range all {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				s := open()
				if err := s.Create(ctx(), working("t")); err != nil {
					t.Fatalf("Create: %v", err)
				}
				if from != protocolcodec.TaskWorking {
					if _, err := s.Transition(ctx(), "t", from, ""); err != nil {
						t.Skipf("cannot reach %q from working in one hop", from)
					}
				}
				_, err := s.Transition(ctx(), "t", to, "")
				legal := from.CanTransitionTo(to) || from == to
				if legal && err != nil {
					t.Fatalf("legal transition %s->%s errored: %v", from, to, err)
				}
				if !legal && !errors.Is(err, tasks.ErrIllegalTransition) {
					t.Fatalf("illegal %s->%s: want ErrIllegalTransition, got %v", from, to, err)
				}
			})
		}
	}
}

func testTransitionSameTerminalNoOp(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Transition(ctx(), "t", protocolcodec.TaskCancelled, "first"); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	// A late terminal write onto an already-cancelled task must not error.
	if _, err := s.Transition(ctx(), "t", protocolcodec.TaskCancelled, "late"); err != nil {
		t.Fatalf("redundant cancelled transition must be a no-op, got %v", err)
	}
}

func testTransitionWorkingRefreshesMessage(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A working->working transition refreshes the status message — the
	// TaskHandle progress path.
	rec, err := s.Transition(ctx(), "t", protocolcodec.TaskWorking, "50% complete")
	if err != nil {
		t.Fatalf("working->working transition: %v", err)
	}
	if rec.StatusMessage != "50% complete" {
		t.Fatalf("status message = %q, want refreshed", rec.StatusMessage)
	}
	got, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StatusMessage != "50% complete" {
		t.Fatalf("refreshed message did not persist: %q", got.StatusMessage)
	}
}

func testSetResult(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := tasks.TaskResult{Payload: []byte(`{"ok":true}`)}
	if err := s.SetResult(ctx(), "t", want); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	got, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Result.Payload) != string(want.Payload) {
		t.Fatalf("result payload = %q, want %q", got.Result.Payload, want.Payload)
	}
}

func testFinalizeAtomic(t *testing.T, open OpenFunc) {
	s := open()
	atomic := atomicFinalize(t, s)
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatal(err)
	}
	request := tasks.InputRequest{Key: "approval", Method: tasks.InputMethodElicitation,
		Payload: []byte(`{"method":"elicitation/create","params":{"message":"Approve?"}}`)}
	if err := s.AddInputRequest(ctx(), "t", request); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type outcome struct {
		status  protocolcodec.TaskStatus
		applied bool
		err     error
	}
	outcomes := make(chan outcome, 2)
	for _, candidate := range []struct {
		status protocolcodec.TaskStatus
		result tasks.TaskResult
	}{
		{protocolcodec.TaskCompleted, tasks.TaskResult{Payload: []byte(`{"winner":"completed"}`)}},
		{protocolcodec.TaskCancelled, tasks.TaskResult{Err: "task cancelled"}},
	} {
		wg := candidate
		go func() {
			<-start
			_, applied, err := atomic.Finalize(ctx(), "t", wg.status, string(wg.status), wg.result)
			outcomes <- outcome{wg.status, applied, err}
		}()
	}
	close(start)
	first, second := <-outcomes, <-outcomes
	if first.err != nil || second.err != nil || first.applied == second.applied {
		t.Fatalf("finalize outcomes = %#v, %#v", first, second)
	}
	rec, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.InputRequests) != 0 {
		t.Fatalf("terminal record retained input requests: %#v", rec.InputRequests)
	}
	switch rec.Status {
	case protocolcodec.TaskCompleted:
		if string(rec.Result.Payload) != `{"winner":"completed"}` || rec.Result.Err != "" {
			t.Fatalf("completed record has mismatched result: %#v", rec.Result)
		}
	case protocolcodec.TaskCancelled:
		if rec.Result.Err != "task cancelled" || len(rec.Result.Payload) != 0 {
			t.Fatalf("cancelled record has mismatched result: %#v", rec.Result)
		}
	default:
		t.Fatalf("unexpected terminal status %q", rec.Status)
	}
}

func testInputRequestValidation(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatal(err)
	}
	for _, req := range []tasks.InputRequest{
		{Key: "missing", Method: tasks.InputMethodRoots, Payload: []byte(`{}`)},
		{Key: "mismatch", Method: tasks.InputMethodRoots, Payload: []byte(`{"method":"elicitation/create","params":{"message":"x"}}`)},
	} {
		if err := s.AddInputRequest(ctx(), "t", req); !errors.Is(err, tasks.ErrInvalidParams) {
			t.Fatalf("AddInputRequest(%s) error = %v, want ErrInvalidParams", req.Key, err)
		}
	}
	rec, err := s.Get(ctx(), "t")
	if err != nil || rec.Status != protocolcodec.TaskWorking || len(rec.InputRequests) != 0 {
		t.Fatalf("invalid request changed record: %#v, %v", rec, err)
	}
}

func testListPaginates(t *testing.T, open OpenFunc) {
	s := open()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		rec := working(id)
		// Stagger CreatedAt so the durable driver's chronological sort is total.
		rec.CreatedAt = rec.CreatedAt.Add(time.Duration(id[0]) * time.Millisecond)
		if err := s.Create(ctx(), rec); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	seen := 0
	cursor := ""
	pages := 0
	for {
		page, next, err := s.List(ctx(), cursor, 2)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		seen += len(page)
		pages++
		if next == "" {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("List paged more than 10 times for 5 records — cursor not terminating")
		}
	}
	if seen != 5 {
		t.Fatalf("List returned %d records across pages, want 5", seen)
	}
}

func testListByAuthContextScopes(t *testing.T, open OpenFunc) {
	s := open()
	mk := func(id, authCtx string) {
		rec := working(id)
		rec.AuthContext = authCtx
		if err := s.Create(ctx(), rec); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	mk("a1", "alice")
	mk("a2", "alice")
	mk("b1", "bob")

	alice, _, err := s.ListByAuthContext(ctx(), "alice", "", 0)
	if err != nil {
		t.Fatalf("ListByAuthContext alice: %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("alice sees %d tasks, want 2", len(alice))
	}
	for _, r := range alice {
		if r.AuthContext != "alice" {
			t.Fatalf("alice's listing leaked task %q from context %q", r.ID, r.AuthContext)
		}
	}
	bob, _, err := s.ListByAuthContext(ctx(), "bob", "", 0)
	if err != nil {
		t.Fatalf("ListByAuthContext bob: %v", err)
	}
	if len(bob) != 1 || bob[0].ID != "b1" {
		t.Fatalf("bob's listing = %v, want [b1]", bob)
	}
}

func testDeleteIdempotent(t *testing.T, open OpenFunc) {
	s := open()
	if err := s.Create(ctx(), working("t")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx(), "t"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx(), "t"); !errors.Is(err, tasks.ErrTaskNotFound) {
		t.Fatalf("Get after Delete: want ErrTaskNotFound, got %v", err)
	}
	// Deleting an absent task is a nil-error no-op.
	if err := s.Delete(ctx(), "t"); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if err := s.Delete(ctx(), "never-existed"); err != nil {
		t.Fatalf("Delete of an unknown task: %v", err)
	}
}

func testPurgeExpired(t *testing.T, open OpenFunc) {
	s := open()
	now := time.Now().UTC()

	// An expired terminal task.
	expired := working("expired")
	expired.CreatedAt = now.Add(-2 * time.Hour)
	expired.UpdatedAt = expired.CreatedAt
	expired.ExpiresAt = now.Add(-1 * time.Hour)
	if err := s.Create(ctx(), expired); err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	if _, _, err := atomicFinalize(t, s).Finalize(ctx(), expired.ID, protocolcodec.TaskCompleted, "done", tasks.TaskResult{Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Finalize expired: %v", err)
	}
	// An active task is retained even after its TTL so its worker remains
	// addressable and cancellable by the engine.
	activeExpired := working("active-expired")
	activeExpired.ExpiresAt = now.Add(-time.Hour)
	if err := s.Create(ctx(), activeExpired); err != nil {
		t.Fatalf("Create active expired: %v", err)
	}
	// A live task with a future expiry.
	live := working("live")
	live.ExpiresAt = now.Add(1 * time.Hour)
	if err := s.Create(ctx(), live); err != nil {
		t.Fatalf("Create live: %v", err)
	}
	// An unlimited-retention task (zero ExpiresAt) — never reaped.
	unlimited := working("unlimited")
	if err := s.Create(ctx(), unlimited); err != nil {
		t.Fatalf("Create unlimited: %v", err)
	}

	n, err := s.PurgeExpired(ctx(), now)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("PurgeExpired reaped %d tasks, want 1", n)
	}
	if _, err := s.Get(ctx(), "expired"); !errors.Is(err, tasks.ErrTaskNotFound) {
		t.Fatalf("expired task survived the purge")
	}
	if _, err := s.Get(ctx(), "live"); err != nil {
		t.Fatalf("live task was reaped: %v", err)
	}
	if _, err := s.Get(ctx(), "active-expired"); err != nil {
		t.Fatalf("active expired task was reaped: %v", err)
	}
	if _, err := s.Get(ctx(), "unlimited"); err != nil {
		t.Fatalf("unlimited-retention task was reaped: %v", err)
	}
}

func testExpiredActiveSnapshot(t *testing.T, open OpenFunc) {
	s := open()
	now := time.Now().UTC()
	for _, rec := range []tasks.TaskRecord{
		func() tasks.TaskRecord { r := working("expired"); r.ExpiresAt = now.Add(-time.Minute); return r }(),
		func() tasks.TaskRecord { r := working("live"); r.ExpiresAt = now.Add(time.Minute); return r }(),
		working("unlimited"),
	} {
		if err := s.Create(ctx(), rec); err != nil {
			t.Fatal(err)
		}
	}
	terminal := working("terminal-expired")
	terminal.ExpiresAt = now.Add(-time.Minute)
	if err := s.Create(ctx(), terminal); err != nil {
		t.Fatal(err)
	}
	if _, _, err := atomicFinalize(t, s).Finalize(ctx(), terminal.ID, protocolcodec.TaskCompleted, "done", tasks.TaskResult{}); err != nil {
		t.Fatal(err)
	}
	recs, err := expiredActive(t, s).ExpiredActive(ctx(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != "expired" {
		t.Fatalf("expired active snapshot = %#v, want expired only", recs)
	}
	if err := s.Create(ctx(), func() tasks.TaskRecord { r := working("created-after"); r.ExpiresAt = now.Add(-time.Minute); return r }()); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("snapshot changed after later creation: %#v", recs)
	}
}

func testTTLRoundTrip(t *testing.T, open OpenFunc) {
	s := open()
	ttl := int64(60000)
	rec := working("t")
	rec.RequestedTTL = &ttl
	rec.TTL = &ttl
	rec.ExpiresAt = rec.CreatedAt.Add(time.Minute)
	if err := s.Create(ctx(), rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TTL == nil || *got.TTL != ttl {
		t.Fatalf("TTL did not round-trip: %v", got.TTL)
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt did not round-trip")
	}
	if !got.IsExpired(got.CreatedAt.Add(2 * time.Minute)) {
		t.Fatal("IsExpired should be true two minutes past a one-minute TTL")
	}
}

func testScalarPointersCloned(t *testing.T, open OpenFunc) {
	s := open()
	requested, ttl, poll := int64(10), int64(20), int64(30)
	rec := working("t")
	rec.RequestedTTL, rec.TTL, rec.PollInterval = &requested, &ttl, &poll
	if err := s.Create(ctx(), rec); err != nil {
		t.Fatal(err)
	}
	requested, ttl, poll = 100, 200, 300
	got, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if *got.RequestedTTL != 10 || *got.TTL != 20 || *got.PollInterval != 30 {
		t.Fatalf("caller mutation reached store: %#v", got)
	}
	*got.RequestedTTL, *got.TTL, *got.PollInterval = 101, 201, 301
	again, err := s.Get(ctx(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if *again.RequestedTTL != 10 || *again.TTL != 20 || *again.PollInterval != 30 {
		t.Fatalf("returned pointer mutation reached store: %#v", again)
	}
}

func testCancelledContexts(t *testing.T, open OpenFunc) {
	s := open()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Create(ctx(), working("existing")); err != nil {
		t.Fatal(err)
	}
	input := tasks.InputRequest{Key: "roots", Method: tasks.InputMethodRoots, Payload: []byte(`{"method":"roots/list","params":{}}`)}
	cases := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { return s.Create(cancelled, working("cancelled")) }},
		{"CreateWithConcurrencyLimit", func() error { return atomicCreate(t, s).CreateWithConcurrencyLimit(cancelled, working("limited"), 1) }},
		{"Get", func() error { _, err := s.Get(cancelled, "existing"); return err }},
		{"Transition", func() error {
			_, err := s.Transition(cancelled, "existing", protocolcodec.TaskWorking, "changed")
			return err
		}},
		{"SetResult", func() error { return s.SetResult(cancelled, "existing", tasks.TaskResult{Err: "changed"}) }},
		{"Finalize", func() error {
			_, _, err := atomicFinalize(t, s).Finalize(cancelled, "existing", protocolcodec.TaskCompleted, "done", tasks.TaskResult{})
			return err
		}},
		{"AddInputRequest", func() error { return s.AddInputRequest(cancelled, "existing", input) }},
		{"ApplyInputResponses", func() error {
			_, _, err := s.ApplyInputResponses(cancelled, "existing", map[string]tasks.TaskInputResponse{})
			return err
		}},
		{"List", func() error { _, _, err := s.List(cancelled, "", 0); return err }},
		{"ListByAuthContext", func() error { _, _, err := s.ListByAuthContext(cancelled, "", "", 0); return err }},
		{"Delete", func() error { return s.Delete(cancelled, "existing") }},
		{"PurgeExpired", func() error { _, err := s.PurgeExpired(cancelled, time.Now()); return err }},
		{"ExpiredActive", func() error { _, err := expiredActive(t, s).ExpiredActive(cancelled, time.Now()); return err }},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s with cancelled context error = %v", tc.name, err)
		}
	}
	for _, id := range []string{"cancelled", "limited"} {
		if _, err := s.Get(ctx(), id); !errors.Is(err, tasks.ErrTaskNotFound) {
			t.Fatalf("cancelled create %q mutated store: %v", id, err)
		}
	}
	rec, err := s.Get(ctx(), "existing")
	if err != nil || rec.Status != protocolcodec.TaskWorking || rec.StatusMessage != "" || rec.Result.Err != "" || len(rec.InputRequests) != 0 {
		t.Fatalf("cancelled operation mutated record: %#v, %v", rec, err)
	}
}

// testConcurrentUse proves a single TaskStore is safe for concurrent use — the
// reusable-artifact guarantee (CLAUDE.md §5, §14). Meaningful under -race.
func testConcurrentUse(t *testing.T, open OpenFunc) {
	s := open()
	scanner := expiredActive(t, s)
	const workers = 12
	const perWorker = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("w%02d-t%02d", w, i)
				rec := working(id)
				rec.AuthContext = fmt.Sprintf("ctx-%d", w)
				if err := s.Create(ctx(), rec); err != nil {
					errCh <- fmt.Errorf("worker %d Create: %w", w, err)
					return
				}
				if _, err := s.Transition(ctx(), id, protocolcodec.TaskCompleted, "done"); err != nil {
					errCh <- fmt.Errorf("worker %d Transition: %w", w, err)
					return
				}
				if _, err := s.Get(ctx(), id); err != nil {
					errCh <- fmt.Errorf("worker %d Get: %w", w, err)
					return
				}
				if _, _, err := s.ListByAuthContext(ctx(), fmt.Sprintf("ctx-%d", w), "", 0); err != nil {
					errCh <- fmt.Errorf("worker %d List: %w", w, err)
					return
				}
				if _, err := scanner.ExpiredActive(ctx(), time.Now()); err != nil {
					errCh <- fmt.Errorf("worker %d ExpiredActive: %w", w, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	all, _, err := s.List(ctx(), "", 1000)
	if err != nil {
		t.Fatalf("final List: %v", err)
	}
	if want := workers * perWorker; len(all) != want {
		t.Fatalf("after concurrent creates got %d tasks, want %d", len(all), want)
	}
}
