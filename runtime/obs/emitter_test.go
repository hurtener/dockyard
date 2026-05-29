package obs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNopEmitter(t *testing.T) {
	t.Parallel()
	// NopEmitter must accept any event without panicking and without effect.
	NopEmitter{}.Emit(context.Background(), mkEvent(1))
	NopEmitter{}.Emit(context.Background(), Event{}) // even a malformed one
}

func TestOpen_RingBufferDriver(t *testing.T) {
	t.Parallel()
	e, err := Open("ringbuffer", "")
	if err != nil {
		t.Fatalf("Open(ringbuffer): %v", err)
	}
	rb, ok := e.(*RingBuffer)
	if !ok {
		t.Fatalf("ringbuffer driver returned %T, want *RingBuffer", e)
	}
	if rb.Cap() != DefaultRingCapacity {
		t.Errorf("default cap = %d, want %d", rb.Cap(), DefaultRingCapacity)
	}
}

func TestOpen_RingBufferDriver_Capacity(t *testing.T) {
	t.Parallel()
	e, err := Open("ringbuffer", "64")
	if err != nil {
		t.Fatalf("Open(ringbuffer,64): %v", err)
	}
	if rb := e.(*RingBuffer); rb.Cap() != 64 {
		t.Errorf("cap = %d, want 64", rb.Cap())
	}
}

func TestOpen_RingBufferDriver_BadConfig(t *testing.T) {
	t.Parallel()
	if _, err := Open("ringbuffer", "not-a-number"); err == nil {
		t.Error("Open with a bad capacity config must error")
	}
	if _, err := Open("ringbuffer", "-5"); err == nil {
		t.Error("Open with a negative capacity must error")
	}
}

func TestOpen_UnknownDriver(t *testing.T) {
	t.Parallel()
	if _, err := Open("nonexistent", ""); err == nil {
		t.Error("Open of an unknown driver must error")
	}
}

func TestDrivers_IncludesRingBuffer(t *testing.T) {
	t.Parallel()
	found := false
	for _, d := range Drivers() {
		if d == "ringbuffer" {
			found = true
		}
	}
	if !found {
		t.Errorf("Drivers() = %v, must include the ring-buffer driver", Drivers())
	}
}

func TestRegisterDriver_DuplicatePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate driver name must panic")
		}
	}()
	RegisterDriver("ringbuffer", func(string) (Emitter, error) { return NopEmitter{}, nil })
}

func TestRegisterDriver_NilFactoryPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("registering a nil factory must panic")
		}
	}()
	RegisterDriver("obs-test-nil-factory", nil)
}

func TestFanOut_DeliversToEveryDriver(t *testing.T) {
	t.Parallel()
	a := NewRingBuffer(10)
	b := NewRingBuffer(10)
	fo := NewFanOut(a, b, nil) // nil driver dropped
	for i := 0; i < 3; i++ {
		fo.Emit(context.Background(), mkEvent(i))
	}
	if a.Len() != 3 || b.Len() != 3 {
		t.Errorf("FanOut must deliver to every driver: a=%d b=%d want 3,3", a.Len(), b.Len())
	}
}

// closeRecorder is a Closer-implementing Emitter for FanOut.Close coverage.
type closeRecorder struct {
	closed bool
	err    error
}

func (c *closeRecorder) Emit(context.Context, Event) {}
func (c *closeRecorder) Close() error {
	c.closed = true
	return c.err
}

func TestFanOut_CloseClosesEveryCloser(t *testing.T) {
	t.Parallel()
	c1 := &closeRecorder{}
	c2 := &closeRecorder{}
	fo := NewFanOut(c1, NopEmitter{}, c2)
	if err := fo.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !c1.closed || !c2.closed {
		t.Error("Close must close every Closer driver")
	}
}

func TestFanOut_CloseJoinsErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	c1 := &closeRecorder{err: boom}
	c2 := &closeRecorder{err: errors.New("bang")}
	fo := NewFanOut(c1, c2)
	if err := fo.Close(); err == nil {
		t.Error("Close must surface driver errors")
	}
}

// stalledEmitter blocks in Emit until released — a deliberately slow consumer.
type stalledEmitter struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (s *stalledEmitter) Emit(context.Context, Event) {
	s.once.Do(func() { close(s.started) })
	<-s.release
}

// TestFanOut_SlowDriverIsItsOwnProblem documents the FanOut contract: FanOut is
// non-blocking only because each driver's Emit is non-blocking. A driver that
// violates the contract (blocks) blocks the FanOut — which is why the V1
// drivers (ring buffer here; SSE in Phase 16) are all themselves non-blocking.
// The real non-blocking proof for the shipped driver is
// TestRingBuffer_SlowConsumerNeverBlocksEmit below.
func TestFanOut_SlowDriverIsItsOwnProblem(t *testing.T) {
	t.Parallel()
	slow := &stalledEmitter{release: make(chan struct{}), started: make(chan struct{})}
	fast := NewRingBuffer(10)
	fo := NewFanOut(slow, fast)
	done := make(chan struct{})
	go func() {
		fo.Emit(context.Background(), mkEvent(1))
		close(done)
	}()
	<-slow.started
	select {
	case <-done:
		t.Error("FanOut returned while a driver was still stalled — unexpected")
	case <-time.After(20 * time.Millisecond):
		// Expected: a contract-violating driver blocks FanOut.
	}
	close(slow.release)
	<-done
}

// TestRingBuffer_SlowConsumerNeverBlocksEmit is the binding acceptance-criterion
// proof: "the emitter never blocks on a slow consumer". A consumer that never
// reads from the ring buffer cannot stall an emitter — Emit only overwrites the
// oldest slot and returns. We emit far more events than the ring holds, with no
// consumer at all, and assert every Emit returns promptly.
func TestRingBuffer_SlowConsumerNeverBlocksEmit(t *testing.T) {
	t.Parallel()
	const emits = 100000
	r := NewRingBuffer(8) // tiny ring, no consumer

	// Build the event ONCE, outside the timed loop. mkEvent does two
	// crypto-random ID generations (NewTrace + newEventID); doing that
	// `emits` times inside the deadline was measuring mkEvent's RNG cost, not
	// Emit, and under `-race` + a loaded parallel suite that occasionally
	// blew the wall clock — the flake. Emit copies the event into the ring,
	// so one reused event still drives `emits` non-blocking writes and the
	// Len/Dropped assertions (which do not depend on per-event identity).
	ev := mkEvent(1)
	done := make(chan struct{})
	go func() {
		for i := 0; i < emits; i++ {
			r.Emit(context.Background(), ev)
		}
		close(done)
	}()
	// Emit is a bounded mutex + slice write with no wait, so it can only fail
	// this test by blocking forever (which hangs until Go's overall test
	// timeout). The deadline therefore only needs to distinguish
	// blocked-forever from slow; it is generous so scheduler starvation under
	// a loaded `-race` run never trips it.
	select {
	case <-done:
		// Every Emit returned; the ring never blocked despite no consumer.
	case <-time.After(30 * time.Second):
		t.Fatal("Emit blocked with no consumer — the emitter must never block")
	}
	if r.Len() != 8 {
		t.Errorf("ring Len = %d, want 8 (bounded)", r.Len())
	}
	if r.Dropped() != emits-8 {
		t.Errorf("Dropped = %d, want %d", r.Dropped(), emits-8)
	}
}
