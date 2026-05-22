package obs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// sseDriverName is the registered driver name of the out-of-band SSE sink.
const sseDriverName = "sse"

// defaultSSEAddr is the SSESink listen address when the driver config is empty:
// an OS-assigned port on the IPv4 loopback. It is loopback-only by construction
// — the sink is a dev surface and is never reachable off-localhost (CLAUDE.md
// §7, P4).
const defaultSSEAddr = "127.0.0.1:0"

// sseSubscriberBuffer is the per-subscriber bounded send queue. A subscriber
// whose queue fills (a slow or stalled consumer) has events DROPPED — it never
// blocks the runtime's emit path (CLAUDE.md §8). It is sized for a burst of
// interactive activity without unbounded memory growth.
const sseSubscriberBuffer = 256

// init registers the SSE-sink driver behind the obs emitter seam (CLAUDE.md
// §4.4). Its config string is the loopback listen address ("" → defaultSSEAddr).
// A non-loopback address is rejected — the sink is localhost-only.
func init() {
	RegisterDriver(sseDriverName, func(cfg string) (Emitter, error) {
		return NewSSESink(cfg)
	})
}

// errSSENonLoopback is returned when an SSESink is asked to bind a
// non-loopback address. The SSE sink is a dev-mode surface and MUST NOT be
// reachable off-localhost (CLAUDE.md §7, P4).
type errSSENonLoopback struct{ addr string }

func (e errSSENonLoopback) Error() string {
	return fmt.Sprintf(
		"dockyard/runtime/obs: SSE sink refuses non-loopback bind address %q "+
			"(the obs SSE sink is dev-mode-only and localhost-bound)", e.addr)
}

// SSESink is the out-of-band, localhost-bound Server-Sent-Events obs/v1 emitter
// driver (RFC §11.3). It streams the live obs/v1 event stream to dev tooling —
// the inspector consumes it in Wave 8 — over its OWN loopback HTTP listener.
//
// It is out-of-band by design: when the MCP transport is stdio, obs events go
// out THIS separate SSE channel and never touch os.Stdout/os.Stdin, so a stdio
// server's JSON-RPC pipe is never corrupted (brief 05 §2.2, §3.3). The sink
// holds no reference to the MCP transport; it cannot write to the protocol pipe.
//
// SSESink is a reusable concurrent artifact: Emit is safe from many goroutines,
// and many HTTP subscribers can connect and disconnect concurrently. Emit is
// NON-BLOCKING: each subscriber has a bounded send queue; a slow or stalled
// subscriber has events dropped rather than stalling the emit path (CLAUDE.md
// §8). The number of dropped events is exposed via [SSESink.Dropped].
type SSESink struct {
	addr     string
	listener net.Listener
	server   *http.Server

	mu      sync.Mutex
	subs    map[*sseSubscriber]struct{}
	closed  bool
	dropped int64
}

// sseSubscriber is one connected SSE client. ch is its bounded send queue; done
// is closed when the subscriber's HTTP handler returns so Emit stops targeting
// a departed subscriber.
type sseSubscriber struct {
	ch   chan Event
	done chan struct{}
}

// NewSSESink constructs a localhost SSE sink listening on addr and starts its
// HTTP listener. An empty addr uses [defaultSSEAddr] (an OS-assigned loopback
// port). A non-loopback addr is rejected with [errSSENonLoopback] — the sink is
// dev-mode-only and localhost-bound (CLAUDE.md §7).
//
// The returned sink is ready for Emit immediately; the live listen address
// (with the resolved port) is available via [SSESink.Addr].
func NewSSESink(addr string) (*SSESink, error) {
	if addr == "" {
		addr = defaultSSEAddr
	}
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/obs: SSE sink listen %q: %w", addr, err)
	}
	s := &SSESink{
		addr:     ln.Addr().String(),
		listener: ln,
		subs:     map[*sseSubscriber]struct{}{},
	}
	mux := http.NewServeMux()
	mux.Handle("/obs/v1/stream", s.streamHandler())
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		// Serve returns ErrServerClosed on a clean Close; any other error means
		// the listener died — there is nothing to recover at process scope, the
		// sink simply stops accepting subscribers. Emit stays safe regardless.
		_ = s.server.Serve(ln)
	}()
	return s, nil
}

// requireLoopback verifies addr's host resolves to a loopback address. An empty
// or unspecified host (e.g. ":0") is rejected: the sink must bind an explicit
// loopback interface, never a wildcard that would be reachable off-localhost.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return errSSENonLoopback{addr}
	}
	if host == "" {
		// ":0" / ":port" binds every interface — not loopback-only.
		return errSSENonLoopback{addr}
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errSSENonLoopback{addr}
	}
	return nil
}

// Addr returns the sink's resolved listen address, including the OS-assigned
// port when the construction address used port 0.
func (s *SSESink) Addr() string { return s.addr }

// Handler returns the SSE HTTP handler. The sink already serves it on its own
// listener at /obs/v1/stream; Handler is exported so the inspector (Wave 8) can
// additionally mount the live stream on its own localhost mux.
func (s *SSESink) Handler() http.Handler { return s.streamHandler() }

// streamHandler is the SSE endpoint: it registers a subscriber, writes the SSE
// preamble, and copies events from the subscriber's bounded queue to the wire
// until the client disconnects or the sink closes.
func (s *SSESink) streamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		sub := &sseSubscriber{
			ch:   make(chan Event, sseSubscriberBuffer),
			done: make(chan struct{}),
		}
		if !s.addSubscriber(sub) {
			http.Error(w, "obs SSE sink closed", http.StatusServiceUnavailable)
			return
		}
		defer s.removeSubscriber(sub)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		// A retry hint plus an initial comment so a client knows the stream is
		// live before the first event.
		_, _ = w.Write([]byte("retry: 2000\n: obs/v1 stream open\n\n"))
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.done:
				// The sink closed, or this subscriber was deregistered — return
				// so http.Server.Shutdown can complete.
				return
			case ev, ok := <-sub.ch:
				if !ok {
					return
				}
				if !writeSSEEvent(w, ev) {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// writeSSEEvent writes one obs/v1 event as an SSE message. The SSE `event:`
// field carries the event kind so a consumer can filter without parsing the
// body; the `data:` field carries the canonical obs/v1 JSON. A marshal failure
// drops the event silently — observability never fails (P2). It reports whether
// the write succeeded.
func writeSSEEvent(w http.ResponseWriter, ev Event) bool {
	body, err := json.Marshal(ev)
	if err != nil {
		return true // skip this event, keep the stream alive
	}
	// obs/v1 events are single-line JSON, so no multi-line data: splitting is
	// needed; guard anyway by never embedding a raw newline.
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Kind, body)
	if _, err := w.Write([]byte(frame)); err != nil {
		return false
	}
	return true
}

// addSubscriber registers sub. It returns false if the sink is already closed.
func (s *SSESink) addSubscriber(sub *sseSubscriber) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.subs[sub] = struct{}{}
	return true
}

// removeSubscriber deregisters sub and signals its goroutine to stop. It is
// idempotent — a subscriber removed by Close is removed again here harmlessly.
func (s *SSESink) removeSubscriber(sub *sseSubscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.done)
	}
}

// Emit fans e out to every connected subscriber. It is NON-BLOCKING: each
// subscriber has a bounded queue and a full queue means the event is DROPPED
// for that subscriber — a slow or stalled consumer never stalls the runtime
// (CLAUDE.md §8). A malformed event is dropped silently (P2). Emit takes the
// sink lock only for the duration of a non-blocking channel send per subscriber,
// never for I/O.
func (s *SSESink) Emit(_ context.Context, e Event) {
	if !e.valid() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for sub := range s.subs {
		select {
		case sub.ch <- e:
		case <-sub.done:
			// Subscriber departed between Emit and now — skip it.
		default:
			// Bounded queue full: drop for this slow subscriber, never block.
			s.dropped++
		}
	}
}

// Subscribers returns the current count of connected SSE subscribers.
func (s *SSESink) Subscribers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// Dropped returns the total number of per-subscriber event drops caused by a
// full subscriber queue — the "events lost to a slow SSE consumer" signal. It
// is monotonically non-decreasing.
func (s *SSESink) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close shuts the SSE sink down: it stops the HTTP listener and signals every
// connected subscriber's handler to return. It is idempotent (CLAUDE.md §5 —
// the Closer contract) and safe to call concurrently with Emit. After Close,
// Emit is a no-op.
func (s *SSESink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for sub := range s.subs {
		delete(s.subs, sub)
		close(sub.done)
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.server.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("dockyard/runtime/obs: SSE sink shutdown: %w", err)
	}
	return nil
}

// compile-time guards: SSESink is an Emitter and a Closer.
var (
	_ Emitter = (*SSESink)(nil)
	_ Closer  = (*SSESink)(nil)
)
