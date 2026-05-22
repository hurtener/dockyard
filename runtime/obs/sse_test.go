package obs

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestEvent builds a structurally valid obs/v1 event for transport tests.
func newTestEvent(kind EventKind) Event {
	sc := NewTrace()
	return Event{
		SchemaVersion: SchemaVersion,
		ID:            newEventID(),
		Timestamp:     time.Now().UTC(),
		ServerID:      "test-server",
		TraceID:       sc.TraceID,
		SpanID:        sc.SpanID,
		Kind:          kind,
		Phase:         PhaseEmit,
	}
}

func TestNewSSESink_RejectsNonLoopback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"ipv4 loopback", "127.0.0.1:0", false},
		{"localhost name", "localhost:0", false},
		{"ipv6 loopback", "[::1]:0", false},
		{"wildcard host", "0.0.0.0:0", true},
		{"empty host wildcard", ":0", true},
		{"routable address", "192.0.2.1:0", true},
		{"missing port", "127.0.0.1", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewSSESink(tc.addr)
			if tc.wantErr {
				if err == nil {
					_ = s.Close()
					t.Fatalf("NewSSESink(%q): want error, got nil", tc.addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSSESink(%q): unexpected error: %v", tc.addr, err)
			}
			t.Cleanup(func() { _ = s.Close() })
		})
	}
}

func TestNewSSESink_EmptyAddrUsesLoopback(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("")
	if err != nil {
		t.Fatalf("NewSSESink(\"\"): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !strings.HasPrefix(s.Addr(), "127.0.0.1:") {
		t.Fatalf("Addr() = %q, want a 127.0.0.1 loopback address", s.Addr())
	}
}

func TestSSESink_RegisteredDriver(t *testing.T) {
	t.Parallel()
	found := false
	for _, d := range Drivers() {
		if d == sseDriverName {
			found = true
		}
	}
	if !found {
		t.Fatalf("driver %q not registered behind the obs emitter seam", sseDriverName)
	}
	e, err := Open(sseDriverName, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Open(%q): %v", sseDriverName, err)
	}
	if c, ok := e.(Closer); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
	if _, ok := e.(*SSESink); !ok {
		t.Fatalf("Open(%q) returned %T, want *SSESink", sseDriverName, e)
	}
}

// connectSSE opens an SSE subscriber against s and returns a scanner over the
// stream plus a cancel func. It blocks until the stream preamble is read so the
// subscriber is registered before the caller emits.
func connectSSE(t *testing.T, s *SSESink) (*bufio.Scanner, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+s.Addr()+"/obs/v1/stream", nil)
	if err != nil {
		cancel()
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("connect SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	t.Cleanup(func() { _ = resp.Body.Close() })
	// Drain the preamble (retry hint + open comment + blank line).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			break
		}
		if sc.Text() == "" { // blank line ends the preamble block
			break
		}
	}
	return sc, cancel
}

func TestSSESink_StreamsEvents(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sc, cancel := connectSSE(t, s)
	defer cancel()

	// Wait for the subscriber registration to be visible to Emit.
	waitFor(t, 2*time.Second, func() bool { return s.Subscribers() == 1 })

	want := newTestEvent(KindToolCall)
	s.Emit(context.Background(), want)

	gotEvent, gotData := readSSEFrame(t, sc)
	if gotEvent != string(KindToolCall) {
		t.Fatalf("SSE event field = %q, want %q", gotEvent, KindToolCall)
	}
	if !strings.Contains(gotData, want.ID) {
		t.Fatalf("SSE data %q does not carry event ID %q", gotData, want.ID)
	}
	if !strings.Contains(gotData, SchemaVersion) {
		t.Fatalf("SSE data %q does not carry the obs/v1 schema version", gotData)
	}
}

// readSSEFrame reads one event:/data: SSE frame from sc.
func readSSEFrame(t *testing.T, sc *bufio.Scanner) (eventField, dataField string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			t.Fatalf("SSE stream closed before a frame arrived")
		}
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventField = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dataField = strings.TrimPrefix(line, "data: ")
		case line == "":
			if eventField != "" || dataField != "" {
				return eventField, dataField
			}
		}
	}
	t.Fatalf("no SSE frame within deadline")
	return "", ""
}

func TestSSESink_DropsMalformedEvent(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// An invalid event (no IDs) must be dropped silently — no panic.
	s.Emit(context.Background(), Event{Kind: KindToolCall})
}

func TestSSESink_EmitAfterCloseIsNoop(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent close.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// Emit after close must not panic.
	s.Emit(context.Background(), newTestEvent(KindLog))
	if got := s.Subscribers(); got != 0 {
		t.Fatalf("Subscribers() after close = %d, want 0", got)
	}
}

// TestSSESink_StalledSubscriberNeverBlocksEmit proves a deliberately stalled
// subscriber — one that never reads its stream — cannot block the runtime's
// emit path (CLAUDE.md §8). It floods past the bounded per-subscriber queue and
// asserts Emit stays fast and drops are accounted.
func TestSSESink_StalledSubscriberNeverBlocksEmit(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// A subscriber that connects but never reads — its TCP/queue backs up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+s.Addr()+"/obs/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect stalled subscriber: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	waitFor(t, 2*time.Second, func() bool { return s.Subscribers() == 1 })

	// Flood far past the bounded queue and the OS socket buffer; every Emit
	// must return promptly. The flood is large enough that — even if the
	// handler drains a burst into the socket buffer before the stalled reader
	// backs it up — the bounded per-subscriber queue must overflow and drop.
	const flood = sseSubscriberBuffer * 64
	done := make(chan struct{})
	go func() {
		for i := 0; i < flood; i++ {
			s.Emit(context.Background(), newTestEvent(KindToolCall))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked on a stalled SSE subscriber — the emit path is not non-blocking")
	}
	// Drops are the proof the bounded queue shed load rather than blocking. The
	// handler may keep draining for a moment after the flood; poll briefly.
	waitFor(t, 2*time.Second, func() bool { return s.Dropped() > 0 })
}

// TestSSESink_ConcurrentSubscribersAndEmit is the reusable-concurrent-artifact
// proof: many subscribers connect, churn, and disconnect while Emit runs from
// several goroutines. Run under -race.
func TestSSESink_ConcurrentSubscribersAndEmit(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Concurrent emitters.
	for e := 0; e < 4; e++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.Emit(context.Background(), newTestEvent(KindToolCall))
				}
			}
		}()
	}

	// Subscriber connect/disconnect churn.
	for c := 0; c < 16; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 4; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
					"http://"+s.Addr()+"/obs/v1/stream", nil)
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
				cancel()
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// After the churn settles, every subscriber goroutine must have exited.
	waitFor(t, 3*time.Second, func() bool { return s.Subscribers() == 0 })
}

// TestSSESink_CloseDrainsSubscribers proves Close terminates connected
// subscribers and leaves no goroutine leak.
func TestSSESink_CloseDrainsSubscribers(t *testing.T) {
	t.Parallel()
	s, err := NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	_, cancel := connectSSE(t, s)
	defer cancel()
	waitFor(t, 2*time.Second, func() bool { return s.Subscribers() == 1 })

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := s.Subscribers(); got != 0 {
		t.Fatalf("Subscribers() after Close = %d, want 0", got)
	}
}

// waitFor polls cond until it is true or the deadline elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", d)
	}
}
