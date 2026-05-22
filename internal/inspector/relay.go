package inspector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rpcLogCap bounds the in-memory JSON-RPC log ring. The inspector is a dev
// surface; an interactive session's wire traffic fits comfortably, and a bound
// keeps memory flat over a long-running `dockyard dev` session.
const rpcLogCap = 512

// subscriberBuffer is the per-UI-client bounded send queue for the relayed obs
// stream. A slow inspector UI client has events dropped rather than stalling
// the relay — the relay never blocks on a slow consumer (CLAUDE.md §8), exactly
// as runtime/obs's own SSE sink does.
const subscriberBuffer = 256

// RPCDirection is the direction of a logged JSON-RPC message relative to the
// MCP server: a request/notification inbound to the server, or a response
// outbound from it.
type RPCDirection string

const (
	// RPCInbound is a message sent toward the MCP server (a request, a
	// notification, or a ui/* bridge call).
	RPCInbound RPCDirection = "inbound"
	// RPCOutbound is a message returned from the MCP server (a response).
	RPCOutbound RPCDirection = "outbound"
)

// RPCEntry is one entry in the inspector's read-only JSON-RPC log. It is a
// content-free-by-default record of wire traffic — method, direction, and the
// JSON payload — surfaced in the inspector's RPC panel. It is the inspector's
// own type: no raw SDK struct leaks through it (P3).
type RPCEntry struct {
	// Seq is a monotonic sequence number, assigned by the relay on append.
	Seq int64 `json:"seq"`
	// Timestamp is when the entry was logged, in UTC.
	Timestamp time.Time `json:"timestamp"`
	// Direction is inbound (toward the server) or outbound (from it).
	Direction RPCDirection `json:"direction"`
	// Method is the JSON-RPC method (e.g. "tools/call", "ui/initialize"). It is
	// empty for a response entry.
	Method string `json:"method,omitempty"`
	// Payload is the JSON-RPC message payload, JSON-encoded.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Relay is the inspector's read-only bridge between a running MCP server's
// observability surfaces and the inspector UI. It does two things, both
// read-only (RFC §12 — the inspector is never an arbitrary-execution proxy):
//
//   - it is a pure SSE *client* of runtime/obs's obs/v1 SSE sink (P2 — it
//     consumes the public obs/v1 contract, never runtime internals), and fans
//     the stream out to every connected inspector UI client;
//   - it holds a bounded ring of recent JSON-RPC log entries for the RPC panel.
//
// Relay is a reusable concurrent artifact: [Relay.Run] streams from the obs
// sink, many UI clients may [Relay.Subscribe] concurrently, and [Relay.LogRPC]
// is safe from any goroutine.
type Relay struct {
	// obsURL is the obs/v1 SSE stream URL of the attached runtime/obs SSE sink.
	// Empty disables the obs stream — the relay still serves an empty stream.
	obsURL string
	client *http.Client

	mu      sync.Mutex
	subs    map[*subscriber]struct{}
	rpcLog  []RPCEntry
	rpcNext int64
	dropped int64
	closed  bool
}

// subscriber is one connected inspector UI client of the relayed obs stream.
type subscriber struct {
	ch   chan []byte
	done chan struct{}
}

// NewRelay constructs a Relay. obsURL is the obs/v1 SSE stream URL of the
// attached runtime/obs SSE sink (e.g. "http://127.0.0.1:54321/obs/v1/stream");
// an empty obsURL disables obs streaming but the relay still serves an empty
// stream and the RPC log. The relay does not connect until [Relay.Run].
func NewRelay(obsURL string) *Relay {
	return &Relay{
		obsURL: obsURL,
		client: &http.Client{},
		subs:   map[*subscriber]struct{}{},
	}
}

// ObsURL reports the configured obs/v1 stream URL, or "" when obs streaming is
// disabled.
func (r *Relay) ObsURL() string { return r.obsURL }

// Run connects to the obs/v1 SSE sink and fans every received event out to
// connected inspector UI clients until ctx is cancelled. It is the relay's pure
// SSE-client loop. Run reconnects on a dropped upstream connection with a small
// backoff — a dev server restart does not kill the inspector's stream. Run
// returns when ctx is cancelled. With no obsURL configured, Run returns
// immediately.
func (r *Relay) Run(ctx context.Context) {
	if r.obsURL == "" {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		r.streamOnce(ctx)
		// Upstream dropped (or never connected); back off before retrying so a
		// down dev server is not hammered.
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// streamOnce opens one upstream SSE connection and copies frames to subscribers
// until the connection drops or ctx is cancelled.
func (r *Relay) streamOnce(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.obsURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return
	}
	// An SSE stream is newline-delimited frames; the relay forwards the obs/v1
	// `data:` payload of each frame verbatim (it never re-parses the obs event —
	// it is a pure consumer of the public contract).
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		r.fanout([]byte(data))
		if ctx.Err() != nil {
			return
		}
	}
}

// Subscribe registers an inspector UI client for the relayed obs stream. It
// returns a receive channel of obs/v1 event JSON payloads and an unsubscribe
// function. The channel is bounded; a slow consumer has events dropped (counted
// by [Relay.Dropped]) — the relay never blocks (CLAUDE.md §8). The unsubscribe
// function is idempotent.
func (r *Relay) Subscribe() (<-chan []byte, func()) {
	sub := &subscriber{
		ch:   make(chan []byte, subscriberBuffer),
		done: make(chan struct{}),
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		close(sub.done)
		close(sub.ch)
		return sub.ch, func() {}
	}
	r.subs[sub] = struct{}{}
	r.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			r.mu.Lock()
			if _, ok := r.subs[sub]; ok {
				delete(r.subs, sub)
				close(sub.done)
			}
			r.mu.Unlock()
		})
	}
	return sub.ch, unsub
}

// fanout pushes one obs/v1 event payload to every subscriber, non-blocking.
func (r *Relay) fanout(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for sub := range r.subs {
		select {
		case sub.ch <- payload:
		case <-sub.done:
		default:
			// Bounded queue full: drop for this slow UI client, never block.
			r.dropped++
		}
	}
}

// Dropped reports the total obs events dropped to a slow inspector UI client.
// It is monotonically non-decreasing.
func (r *Relay) Dropped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Subscribers reports the count of connected inspector UI stream clients.
func (r *Relay) Subscribers() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

// LogRPC appends a JSON-RPC log entry to the relay's bounded ring. It is safe
// from any goroutine. direction and method classify the entry; payload is the
// raw JSON-RPC message. The relay assigns the sequence number and timestamp.
func (r *Relay) LogRPC(direction RPCDirection, method string, payload json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	entry := RPCEntry{
		Seq:       r.rpcNext,
		Timestamp: time.Now().UTC(),
		Direction: direction,
		Method:    method,
		Payload:   payload,
	}
	r.rpcNext++
	r.rpcLog = append(r.rpcLog, entry)
	if len(r.rpcLog) > rpcLogCap {
		// Drop the oldest; keep the ring bounded.
		r.rpcLog = r.rpcLog[len(r.rpcLog)-rpcLogCap:]
	}
}

// RPCLog returns a snapshot copy of the current JSON-RPC log, oldest first. The
// returned slice is safe for the caller to retain.
func (r *Relay) RPCLog() []RPCEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RPCEntry, len(r.rpcLog))
	copy(out, r.rpcLog)
	return out
}

// Close releases the relay: it deregisters every subscriber and stops accepting
// new ones. It is idempotent. After Close, [Relay.Subscribe] returns a closed
// channel and [Relay.LogRPC] is a no-op.
func (r *Relay) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	for sub := range r.subs {
		delete(r.subs, sub)
		close(sub.done)
	}
	return nil
}

// streamHandler is the inspector's obs/v1 relay SSE endpoint. The inspector UI
// connects to it; it writes the relayed obs/v1 events as an SSE stream. It is
// read-only and localhost-only (the inspector's listener is loopback-bound).
func (r *Relay) streamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		ch, unsub := r.Subscribe()
		defer unsub()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("retry: 2000\n: inspector obs/v1 relay open\n\n"))
		flusher.Flush()

		ctx := req.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case payload, open := <-ch:
				if !open {
					return
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// rpcLogHandler serves the current JSON-RPC log as a JSON array. The inspector
// UI polls it for the RPC panel. It is read-only.
func (r *Relay) rpcLogHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_ = json.NewEncoder(w).Encode(r.RPCLog())
	}
}
