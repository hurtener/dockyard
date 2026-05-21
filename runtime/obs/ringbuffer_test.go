package obs

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mkEvent builds a minimal valid Event for ring-buffer tests; n distinguishes
// events so ordering can be asserted (encoded into the event ID suffix).
func mkEvent(n int) Event {
	sc := NewTrace()
	e := Event{
		SchemaVersion: SchemaVersion,
		ID:            newEventID(),
		Timestamp:     time.Unix(int64(n), 0).UTC(),
		ServerID:      "ring-test",
		TraceID:       sc.TraceID,
		SpanID:        sc.SpanID,
		Kind:          KindToolCall,
		Phase:         PhaseEmit,
	}
	return e
}

func TestRingBuffer_EmitAndRecent(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(10)
	for i := 0; i < 5; i++ {
		r.Emit(context.Background(), mkEvent(i))
	}
	if r.Len() != 5 {
		t.Fatalf("Len = %d, want 5", r.Len())
	}
	got := r.Recent(0)
	if len(got) != 5 {
		t.Fatalf("Recent(0) returned %d events, want 5", len(got))
	}
	// Oldest first.
	for i := 0; i < 5; i++ {
		if got[i].Timestamp.Unix() != int64(i) {
			t.Errorf("Recent[%d] timestamp = %d, want %d", i, got[i].Timestamp.Unix(), i)
		}
	}
}

func TestRingBuffer_Bounded_OverwritesOldest(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(3)
	for i := 0; i < 7; i++ {
		r.Emit(context.Background(), mkEvent(i))
	}
	if r.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (bounded)", r.Len())
	}
	got := r.Recent(0)
	// The three most recent (4,5,6) survive; older ones were overwritten.
	want := []int64{4, 5, 6}
	for i, e := range got {
		if e.Timestamp.Unix() != want[i] {
			t.Errorf("Recent[%d] = %d, want %d", i, e.Timestamp.Unix(), want[i])
		}
	}
	if r.Dropped() != 4 {
		t.Errorf("Dropped = %d, want 4", r.Dropped())
	}
}

func TestRingBuffer_RecentN(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(10)
	for i := 0; i < 8; i++ {
		r.Emit(context.Background(), mkEvent(i))
	}
	got := r.Recent(3)
	if len(got) != 3 {
		t.Fatalf("Recent(3) returned %d, want 3", len(got))
	}
	for i, e := range got {
		if e.Timestamp.Unix() != int64(5+i) {
			t.Errorf("Recent(3)[%d] = %d, want %d", i, e.Timestamp.Unix(), 5+i)
		}
	}
	// Asking for more than retained returns all.
	if len(r.Recent(100)) != 8 {
		t.Errorf("Recent(100) should return all 8")
	}
}

func TestRingBuffer_DropsMalformedEvents(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(10)
	r.Emit(context.Background(), Event{ID: "no-schema"}) // malformed
	if r.Len() != 0 {
		t.Errorf("a malformed event must be dropped, Len = %d", r.Len())
	}
	r.Emit(context.Background(), mkEvent(1)) // valid
	if r.Len() != 1 {
		t.Errorf("a valid event must be retained, Len = %d", r.Len())
	}
}

func TestRingBuffer_RecentReturnsCopy(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(10)
	r.Emit(context.Background(), mkEvent(1))
	got := r.Recent(0)
	got[0].ServerID = "mutated"
	// A second read must be unaffected — Recent returns a fresh copy.
	again := r.Recent(0)
	if again[0].ServerID == "mutated" {
		t.Error("Recent must return a copy; a caller mutation leaked into the ring")
	}
}

func TestRingBuffer_ZeroCapacityPromoted(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(0)
	if r.Cap() != DefaultRingCapacity {
		t.Errorf("Cap = %d, want DefaultRingCapacity %d", r.Cap(), DefaultRingCapacity)
	}
}

func TestRingBuffer_DroppedZeroBeforeWrap(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(5)
	for i := 0; i < 5; i++ {
		r.Emit(context.Background(), mkEvent(i))
	}
	if r.Dropped() != 0 {
		t.Errorf("Dropped = %d before wrap, want 0", r.Dropped())
	}
}

// TestRingBuffer_ConcurrentEmitAndRead is the reusable-concurrent-artifact
// proof (CLAUDE.md §5): many goroutines Emit while others Recent/Len/Dropped,
// all under -race. The ring must never block an emitter and never corrupt.
func TestRingBuffer_ConcurrentEmitAndRead(t *testing.T) {
	t.Parallel()
	r := NewRingBuffer(256)
	const writers, readers, perWriter = 16, 8, 500

	var writersWG, readersWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func() {
			defer writersWG.Done()
			for i := 0; i < perWriter; i++ {
				r.Emit(context.Background(), mkEvent(i))
			}
		}()
	}
	stop := make(chan struct{})
	for rd := 0; rd < readers; rd++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = r.Recent(32)
					_ = r.Len()
					_ = r.Dropped()
				}
			}
		}()
	}
	writersWG.Wait()
	close(stop)
	readersWG.Wait()

	total := writers * perWriter
	if r.Len() > r.Cap() {
		t.Errorf("Len %d exceeded Cap %d", r.Len(), r.Cap())
	}
	if total > r.Cap() && r.Dropped() == 0 {
		t.Error("expected drops after exceeding capacity")
	}
}
