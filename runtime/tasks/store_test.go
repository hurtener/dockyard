package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

func workingRecord(id string) TaskRecord {
	now := time.Now().UTC()
	return TaskRecord{
		ID:        id,
		Status:    protocolcodec.TaskWorking,
		CreatedAt: now,
		UpdatedAt: now,
		Method:    "tools/call",
	}
}

func TestInMemoryStore_CreateGet(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	if err := s.Create(ctx, workingRecord("t1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "t1" || got.Status != protocolcodec.TaskWorking {
		t.Fatalf("unexpected record: %#v", got)
	}
}

func TestInMemoryStore_CreateRejectsNonWorking(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	rec := workingRecord("t1")
	rec.Status = protocolcodec.TaskCompleted
	if err := s.Create(context.Background(), rec); err == nil {
		t.Fatal("Create must reject a task that does not begin in working")
	}
}

func TestInMemoryStore_CreateRejectsDuplicate(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	_ = s.Create(ctx, workingRecord("dup"))
	if err := s.Create(ctx, workingRecord("dup")); err == nil {
		t.Fatal("Create must reject a duplicate task ID")
	}
}

func TestInMemoryStore_GetUnknown(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_, err := s.Get(context.Background(), "nope")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
}

// TestInMemoryStore_LifecycleEnforcement is the binding lifecycle-transition
// table: every legal transition succeeds, every illegal one is a typed error.
func TestInMemoryStore_LifecycleEnforcement(t *testing.T) {
	t.Parallel()
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
				t.Parallel()
				s := NewInMemoryStore()
				ctx := context.Background()
				_ = s.Create(ctx, workingRecord("t"))
				// Drive the task into `from` if it is not already working.
				if from != protocolcodec.TaskWorking {
					if _, err := s.Transition(ctx, "t", from, ""); err != nil {
						t.Skipf("cannot reach %q from working in one hop", from)
					}
				}
				_, err := s.Transition(ctx, "t", to, "")
				legal := from.CanTransitionTo(to) || from == to
				if legal && err != nil {
					t.Fatalf("legal transition %s→%s errored: %v", from, to, err)
				}
				if !legal && !errors.Is(err, ErrIllegalTransition) {
					t.Fatalf("illegal transition %s→%s: want ErrIllegalTransition, got %v", from, to, err)
				}
			})
		}
	}
}

// TestInMemoryStore_SameStatusIsNoOp proves a redundant transition into the
// status the task already holds succeeds — the cooperative-cancellation rule.
func TestInMemoryStore_SameStatusIsNoOp(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	_ = s.Create(ctx, workingRecord("t"))
	if _, err := s.Transition(ctx, "t", protocolcodec.TaskCancelled, "first"); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	// A late terminal write onto an already-cancelled task must not error.
	if _, err := s.Transition(ctx, "t", protocolcodec.TaskCancelled, "late"); err != nil {
		t.Fatalf("redundant cancelled transition should be a no-op, got %v", err)
	}
}

func TestInMemoryStore_ListPagination(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		_ = s.Create(ctx, workingRecord(id))
	}
	page1, next, err := s.List(ctx, "", 2)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page1) != 2 || next == "" {
		t.Fatalf("page 1: len=%d next=%q", len(page1), next)
	}
	page2, next2, err := s.List(ctx, next, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page2) != 2 || next2 == "" {
		t.Fatalf("page 2: len=%d next=%q", len(page2), next2)
	}
	page3, next3, err := s.List(ctx, next2, 2)
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page3) != 1 || next3 != "" {
		t.Fatalf("page 3 (last): len=%d next=%q", len(page3), next3)
	}
}

func TestInMemoryStore_ListRejectsBadCursor(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_, _, err := s.List(context.Background(), "not-a-real-cursor", 10)
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("want ErrInvalidParams for a bad cursor, got %v", err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	for _, i := range []int{0, 1, 42, 1000} {
		tok := encodeCursor(i)
		got, err := decodeCursor(tok)
		if err != nil || got != i {
			t.Fatalf("cursor %d round-trip: got %d err %v", i, got, err)
		}
	}
	if _, err := decodeCursor("garbage"); err == nil {
		t.Fatal("decodeCursor must reject a non-cursor token")
	}
}

func TestCryptoID_UniqueAndStrong(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id, err := CryptoID()
		if err != nil {
			t.Fatalf("CryptoID: %v", err)
		}
		if seen[id] {
			t.Fatalf("CryptoID produced a duplicate: %q", id)
		}
		seen[id] = true
		// 128 bits hex-encoded = 32 hex chars, plus the "task_" prefix.
		if len(id) != len("task_")+2*idBytes {
			t.Fatalf("CryptoID %q has unexpected length %d", id, len(id))
		}
	}
}
