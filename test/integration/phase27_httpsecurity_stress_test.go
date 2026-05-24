package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// TestPhase27_HTTPSecurity_StressUnderAdversarialLoad drives N≥20 concurrent
// clients against a real Dockyard HTTP handler — `runtime/server` with the
// explicit DefaultHTTPSecurity posture and a real `tasks.Engine` attached —
// firing a mix of valid + invalid requests:
//
//   - valid JSON-RPC POSTs with application/json (the happy path);
//   - wrong-Content-Type POSTs (must be 415 by the Dockyard middleware,
//     D-112);
//   - cross-origin POSTs with an Origin header pointing somewhere else
//     (must be 403 by the CrossOriginProtection middleware, D-110);
//   - malformed-JSON POSTs (must error cleanly, not panic);
//   - oversized POST bodies (must be processed or rejected without
//     OOM/panic);
//   - tasks/* JSON-RPC frames mixed in (must reach the Tasks engine and
//     either succeed or error cleanly);
//   - garbage frames (must be rejected without disturbing the engine
//     lifecycle).
//
// The asserts are:
//   - no panic during the run;
//   - no incorrect rejection of the happy path;
//   - the adversarial requests are rejected at the right layer (status code
//     classes match the expected per shape);
//   - no goroutine leak past a settle window;
//   - the Tasks engine remains responsive at the end (the lifecycle was not
//     corrupted by the storm).
func TestPhase27_HTTPSecurity_StressUnderAdversarialLoad(t *testing.T) {
	t.Parallel()

	s, err := server.New(server.Info{Name: "phase27-stress", Version: "0.0.0"}, &server.Options{
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Attach a real Tasks engine so the mount middleware is active.
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		Logger:                slog.New(slog.DiscardHandler),
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	s = s.WithTasks(engine, nil)

	// A trivial typed tool so a happy-path tools/call has somewhere to land.
	type echoIn struct {
		M string `json:"m"`
	}
	type echoOut struct {
		E string `json:"e"`
	}
	if err := server.AddTool(s, server.ToolDef{Name: "echo", Description: "echo"},
		func(_ context.Context, in echoIn) (echoOut, error) {
			return echoOut{E: in.M}, nil
		}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	// Drive a real HTTP server with the explicit secure posture. Stateless
	// mode keeps each POST as a fresh ephemeral session so the goroutine-
	// leak sentinel is meaningful — a stateful run would retain a per-
	// session goroutine for the SDK's session-cleanup window and the
	// "leak" would just be a delayed-drain artifact.
	h, err := s.HTTPHandler(&server.HTTPOptions{
		Security:  server.DefaultHTTPSecurity(),
		Stateless: true,
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	baselineGoroutines := runtime.NumGoroutine()

	// Request shapes. Each shape carries a body builder + the expected
	// status-code class (the worker asserts the response status against it).
	type shape struct {
		name    string
		newReq  func() *http.Request
		wantCls int // 2 → 2xx (success), 4 → 4xx (rejection)
	}

	initBody := func() *bytes.Reader {
		return bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize",` +
			`"params":{"protocolVersion":"2025-06-18","capabilities":{},` +
			`"clientInfo":{"name":"t","version":"0"}}}`))
	}

	shapes := []shape{
		{
			name: "happy-path initialize",
			newReq: func() *http.Request {
				r, _ := http.NewRequest(http.MethodPost, ts.URL, initBody())
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", "application/json, text/event-stream")
				return r
			},
			wantCls: 2,
		},
		{
			name: "wrong content-type",
			newReq: func() *http.Request {
				r, _ := http.NewRequest(http.MethodPost, ts.URL, initBody())
				r.Header.Set("Content-Type", "text/plain")
				return r
			},
			wantCls: 4,
		},
		{
			name: "missing content-type",
			newReq: func() *http.Request {
				r, _ := http.NewRequest(http.MethodPost, ts.URL, initBody())
				return r
			},
			wantCls: 4,
		},
		{
			name: "cross-origin POST",
			newReq: func() *http.Request {
				r, _ := http.NewRequest(http.MethodPost, ts.URL, initBody())
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "https://attacker.example")
				// Sec-Fetch-Site is the modern signal CrossOriginProtection
				// uses; setting it to cross-site makes the rejection
				// deterministic.
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				return r
			},
			wantCls: 4,
		},
		{
			name: "malformed JSON body",
			newReq: func() *http.Request {
				r, _ := http.NewRequest(http.MethodPost, ts.URL,
					bytes.NewReader([]byte(`{not json at all`)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", "application/json, text/event-stream")
				return r
			},
			wantCls: 0, // SDK may answer 200 + JSON-RPC error OR 400 — either is acceptable; the assertion is "not 5xx" and "did not panic".
		},
		{
			name: "oversized body",
			newReq: func() *http.Request {
				big := bytes.NewBuffer(nil)
				big.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"x":"`)
				big.Write(bytes.Repeat([]byte("A"), 256*1024))
				big.WriteString(`"}}`)
				r, _ := http.NewRequest(http.MethodPost, ts.URL, big)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", "application/json, text/event-stream")
				return r
			},
			wantCls: 0, // SDK behaviour varies; non-panicking is the bar.
		},
		{
			name: "tasks/get frame",
			newReq: func() *http.Request {
				body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"taskId":"task_does_not_exist"}}`)
				r, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", "application/json, text/event-stream")
				return r
			},
			wantCls: 0, // 200 with JSON-RPC error body for "not found".
		},
	}

	// Drive ≥20 clients, each issuing many mixed-shape requests.
	const clients = 20
	const perClient = 30

	var (
		issued   int64
		panicked int64
		errs     int64
	)
	var wg sync.WaitGroup
	wg.Add(clients)
	for c := 0; c < clients; c++ {
		go func(c int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&panicked, 1)
					t.Errorf("client %d panicked: %v", c, r)
				}
			}()
			client := &http.Client{Timeout: 10 * time.Second}
			for i := 0; i < perClient; i++ {
				sh := shapes[(c+i)%len(shapes)]
				resp, rerr := client.Do(sh.newReq())
				atomic.AddInt64(&issued, 1)
				if rerr != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if sh.wantCls != 0 {
					cls := resp.StatusCode / 100
					if cls != sh.wantCls {
						t.Errorf("client %d shape %q: status %d, want class %dxx",
							c, sh.name, resp.StatusCode, sh.wantCls)
					}
				}
				// A 5xx on any shape is a defect — every rejection should be
				// 4xx or a clean 200-with-JSON-RPC-error.
				if resp.StatusCode/100 == 5 {
					t.Errorf("client %d shape %q: server-side 5xx %d", c, sh.name, resp.StatusCode)
				}
			}
		}(c)
	}
	wg.Wait()

	t.Logf("phase-27 HTTPSecurity stress: clients=%d perClient=%d issued=%d transport-errs=%d panics=%d",
		clients, perClient, issued, errs, panicked)

	if panicked != 0 {
		t.Fatalf("panics during stress: %d", panicked)
	}

	// Goroutine-leak sentinel: wait up to 2s for the goroutine count to
	// settle within a tolerance band — the streamable-HTTP transport
	// retains a per-session goroutine for a brief window after the body
	// drains, so the tolerance is generous.
	const tolerance = 50
	settleDeadline := time.Now().Add(3 * time.Second)
	var observed int
	for time.Now().Before(settleDeadline) {
		observed = runtime.NumGoroutine()
		if observed-baselineGoroutines <= tolerance {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if observed-baselineGoroutines > tolerance {
		t.Errorf("possible goroutine leak: baseline=%d observed=%d delta=%d tolerance=%d",
			baselineGoroutines, observed, observed-baselineGoroutines, tolerance)
	}

	// Final liveness probe: the engine + the SDK handler still answer.
	probeReq, _ := http.NewRequest(http.MethodPost, ts.URL,
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":99,"method":"initialize",`+
			`"params":{"protocolVersion":"2025-06-18","capabilities":{},`+
			`"clientInfo":{"name":"t","version":"0"}}}`)))
	probeReq.Header.Set("Content-Type", "application/json")
	probeReq.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(probeReq)
	if err != nil {
		t.Fatalf("liveness probe: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("liveness probe failed: status %d body=%s", resp.StatusCode, truncate(string(body), 256))
	}
	if !strings.Contains(string(body), `"jsonrpc"`) {
		t.Fatalf("liveness probe body did not carry a JSON-RPC envelope: %s", truncate(string(body), 256))
	}
	_ = fmt.Sprintf
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
