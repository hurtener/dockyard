package obs

import (
	"context"
	"strconv"
	"sync"
)

// DefaultRingCapacity is the [RingBuffer] capacity used when none is specified
// — enough recent history for an interactive inspector session without
// unbounded memory growth.
const DefaultRingCapacity = 1024

// ringDriverName is the registered driver name of the ring-buffer emitter.
const ringDriverName = "ringbuffer"

// init registers the ring-buffer driver behind the obs emitter seam. Its
// config string is the decimal capacity ("" or "0" → DefaultRingCapacity).
func init() {
	RegisterDriver(ringDriverName, func(cfg string) (Emitter, error) {
		capacity := DefaultRingCapacity
		if cfg != "" {
			n, err := strconv.Atoi(cfg)
			if err != nil || n < 0 {
				return nil, errBadRingConfig{cfg}
			}
			if n > 0 {
				capacity = n
			}
		}
		return NewRingBuffer(capacity), nil
	})
}

type errBadRingConfig struct{ cfg string }

func (e errBadRingConfig) Error() string {
	return "dockyard/runtime/obs: ringbuffer driver: invalid capacity " + strconv.Quote(e.cfg)
}

// RingBuffer is the in-memory, bounded ring-buffer [Emitter] — the obs/v1
// driver Phase 15 ships and the source the inspector pulls recent history from
// (RFC §11.3, brief 05 §3.3).
//
// It is non-blocking by construction: Emit never blocks a caller. When the
// buffer is full the OLDEST event is overwritten — a slow or absent consumer
// can never stall the runtime (CLAUDE.md §8). The number of events dropped this
// way is counted and exposed via [RingBuffer.Dropped] so an inspector can show
// "N events lost".
//
// A RingBuffer is a reusable concurrent artifact: Emit, Recent, Len, and
// Dropped are all safe under concurrent use (CLAUDE.md §5).
type RingBuffer struct {
	mu      sync.Mutex
	buf     []Event
	cap     int
	next    int   // index of the next write
	size    int   // number of events currently held (<= cap)
	written int64 // total events ever accepted (for drop accounting)
}

// NewRingBuffer returns a [RingBuffer] holding at most capacity recent events.
// A capacity <= 0 is promoted to [DefaultRingCapacity].
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingCapacity
	}
	return &RingBuffer{
		buf: make([]Event, capacity),
		cap: capacity,
	}
}

// Emit records e. It is non-blocking: it takes the buffer lock only for the
// duration of a slice write, never for I/O, and never waits on a consumer. A
// malformed event is dropped silently — a buggy emit site never corrupts the
// history or crashes a request (P2). When the buffer is full the oldest event
// is overwritten.
func (r *RingBuffer) Emit(_ context.Context, e Event) {
	if !e.valid() {
		return
	}
	r.mu.Lock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
	r.written++
	r.mu.Unlock()
}

// Recent returns the up-to-n most recent events, oldest first. n <= 0 returns
// every retained event. The returned slice is a fresh copy the caller owns —
// it is never aliased to the ring's storage, so a concurrent Emit cannot mutate
// it (the reusable-artifact rule, CLAUDE.md §5).
func (r *RingBuffer) Recent(n int) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > r.size {
		n = r.size
	}
	out := make([]Event, n)
	// The oldest retained event sits n positions behind the write cursor.
	start := (r.next - n + r.cap*((n/r.cap)+1)) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}

// Len returns the number of events currently retained.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// Cap returns the ring's fixed capacity.
func (r *RingBuffer) Cap() int { return r.cap }

// Dropped returns the number of events overwritten because the buffer was full
// — the "events lost" signal. It is monotonically non-decreasing.
func (r *RingBuffer) Dropped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written <= int64(r.cap) {
		return 0
	}
	return r.written - int64(r.cap)
}
