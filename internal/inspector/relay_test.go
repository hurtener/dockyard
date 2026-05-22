package inspector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRelay_LogRPC_RingBound verifies the JSON-RPC log is a bounded ring: old
// entries are dropped, newest are retained, and sequence numbers stay monotonic.
func TestRelay_LogRPC_RingBound(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	total := rpcLogCap + 50
	for i := 0; i < total; i++ {
		r.LogRPC(RPCInbound, "tools/call", json.RawMessage(`{"jsonrpc":"2.0"}`))
	}
	log := r.RPCLog()
	if len(log) != rpcLogCap {
		t.Fatalf("RPCLog len = %d, want %d", len(log), rpcLogCap)
	}
	// Oldest retained entry's seq is total-rpcLogCap; seqs are monotonic.
	if log[0].Seq != int64(total-rpcLogCap) {
		t.Fatalf("oldest seq = %d, want %d", log[0].Seq, total-rpcLogCap)
	}
	for i := 1; i < len(log); i++ {
		if log[i].Seq != log[i-1].Seq+1 {
			t.Fatalf("non-monotonic seq at %d: %d after %d", i, log[i].Seq, log[i-1].Seq)
		}
	}
}

// TestRelay_LogRPC_Fields verifies a logged entry carries its classification.
func TestRelay_LogRPC_Fields(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	r.LogRPC(RPCOutbound, "ui/initialize", json.RawMessage(`{"id":1}`))
	log := r.RPCLog()
	if len(log) != 1 {
		t.Fatalf("len = %d, want 1", len(log))
	}
	e := log[0]
	if e.Direction != RPCOutbound || e.Method != "ui/initialize" {
		t.Fatalf("entry classification wrong: %+v", e)
	}
	if e.Timestamp.IsZero() {
		t.Fatal("entry timestamp is zero")
	}
}

// TestRelay_StreamFromObsSink connects the relay to a fake obs/v1 SSE sink and
// confirms a UI subscriber receives the relayed event payload — the relay is a
// pure SSE client of the obs/v1 contract (P2).
func TestRelay_StreamFromObsSink(t *testing.T) {
	t.Parallel()
	// A fake obs SSE sink that emits one obs/v1-shaped event.
	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			fl, ok := w.(http.Flusher)
			if !ok {
				t.Error("flusher unsupported")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: tool.call\ndata: {\"id\":\"ev1\"}\n\n"))
			fl.Flush()
			<-req.Context().Done()
		}))
	defer upstream.Close()

	r := NewRelay(upstream.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	ch, unsub := r.Subscribe()
	defer unsub()

	select {
	case payload := <-ch:
		if !strings.Contains(string(payload), `"ev1"`) {
			t.Fatalf("relayed payload wrong: %q", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no relayed event within 3s")
	}
}

// TestRelay_RunNoURL verifies Run returns immediately with no obs URL.
func TestRelay_RunNoURL(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	done := make(chan struct{})
	go func() { r.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return with no obs URL")
	}
}

// TestRelay_SubscribeAfterClose verifies Subscribe on a closed relay yields a
// closed channel.
func TestRelay_SubscribeAfterClose(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	_ = r.Close()
	ch, unsub := r.Subscribe()
	defer unsub()
	if _, open := <-ch; open {
		t.Fatal("Subscribe after Close: channel not closed")
	}
}

// TestRelay_ConcurrentFanout is the reusable-artifact concurrent-reuse test
// (CLAUDE.md §14): many UI subscribers connect and disconnect concurrently
// while the relay fans events. It must be race-clean and never block.
func TestRelay_ConcurrentFanout(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	defer func() { _ = r.Close() }()

	const subscribers = 16
	const events = 200
	var wg sync.WaitGroup

	// Concurrent subscribers, each draining its channel.
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := r.Subscribe()
			defer unsub()
			drained := 0
			deadline := time.After(2 * time.Second)
			for drained < events {
				select {
				case <-ch:
					drained++
				case <-deadline:
					return // a slow consumer simply stops; the relay must not block.
				}
			}
		}()
	}
	// Concurrent fanout from a separate goroutine while LogRPC also runs.
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < events; i++ {
			r.fanout([]byte(`{"id":"ev"}`))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < events; i++ {
			r.LogRPC(RPCInbound, "tools/call", json.RawMessage(`{}`))
		}
	}()
	wg.Wait()
}

// TestRelay_StreamHandler exercises the inspector-facing relay SSE endpoint.
func TestRelay_StreamHandler(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	srv := httptest.NewServer(r.streamHandler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect relay stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("relay stream Content-Type = %q", ct)
	}

	// Wait until the handler has registered its subscriber, then fan one event.
	deadline := time.Now().Add(2 * time.Second)
	for r.Subscribers() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	r.fanout([]byte(`{"id":"streamed"}`))

	got := readUntil(t, resp, "streamed")
	if !strings.Contains(got, "streamed") {
		t.Fatalf("relay SSE frame missing event: %q", got)
	}
}

// readUntil reads SSE body chunks until needle appears or a deadline elapses.
func readUntil(t *testing.T, resp *http.Response, needle string) string {
	t.Helper()
	type result struct{ s string }
	ch := make(chan result, 1)
	go func() {
		var acc strings.Builder
		buf := make([]byte, 512)
		for {
			n, err := resp.Body.Read(buf)
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), needle) || err != nil {
				ch <- result{acc.String()}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		return r.s
	case <-time.After(3 * time.Second):
		t.Fatal("no SSE data within 3s")
		return ""
	}
}

// TestRelay_RPCLogHandler exercises the JSON-RPC log endpoint.
func TestRelay_RPCLogHandler(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	r.LogRPC(RPCInbound, "resources/read", json.RawMessage(`{"uri":"ui://x"}`))
	srv := httptest.NewServer(r.rpcLogHandler())
	defer srv.Close()
	body := func() string {
		resp, err := http.Get(srv.URL) //nolint:gosec // loopback test URL
		if err != nil {
			t.Fatalf("GET rpc log: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var entries []RPCEntry
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			t.Fatalf("decode rpc log: %v", err)
		}
		if len(entries) != 1 || entries[0].Method != "resources/read" {
			t.Fatalf("rpc log payload wrong: %+v", entries)
		}
		return "ok"
	}()
	_ = body
}
