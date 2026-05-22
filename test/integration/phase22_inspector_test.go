// This file is the Phase 22 cross-subsystem integration test (AGENTS.md §17).
// Phase 22's Deps name shipped phases — 09 (runtime/apps), 11 (web/bridge),
// 16 (runtime/obs SSE sink) — and it opens the inspector backend other phases
// build on. The test drives the real seam: a real runtime/server serving a
// real runtime/apps MCP App with a real obs.SSESink behind the emitter seam,
// with the internal/inspector backend relaying the obs/v1 stream. It asserts:
//
//   - the inspector relay streams real obs/v1 events from real tool calls to a
//     UI client (P2 — the inspector is a pure obs/v1 client);
//   - a real ui:// App resource is served by the server and is reachable for
//     the inspector to preview;
//   - the inspector refuses a non-loopback bind (the binding RFC §12 criterion
//     and the CVE-2025-49596 lesson) — the ≥1 failure mode.
//
// No mock at the seam: a real server, a real App, a real SSE sink, the real
// inspector backend. Runs under -race.
package integration

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/inspector"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// phase22Input / phase22Output are the integration tool contract.
type phase22Input struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type phase22Output struct {
	Region string `json:"region"`
	Total  int    `json:"total"`
}

func phase22Handler(_ context.Context, in phase22Input) (tool.Result[phase22Output], error) {
	return tool.Result[phase22Output]{
		Text:       "region " + in.Region + " reported",
		Structured: phase22Output{Region: in.Region, Total: 7},
	}, nil
}

// phase22AppHTML is the minimal MCP App the server registers — a ui:// resource.
const phase22AppHTML = `<!doctype html><html><head><title>Phase 22 App</title>` +
	`</head><body><div id="app">phase-22 app</div></body></html>`

// TestPhase22_InspectorRelaysObsStream wires a real server + App + SSE sink and
// drives the inspector backend, asserting real obs/v1 events from real tool
// calls reach a UI client of the inspector's relay.
func TestPhase22_InspectorRelaysObsStream(t *testing.T) {
	t.Parallel()

	// A real obs SSE sink behind the emitter seam — the inspector's obs source.
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// A real server emitting obs/v1 to the sink.
	srv, err := server.New(
		server.Info{Name: "phase22-server", Version: "0.1.0"},
		&server.Options{Obs: sink, Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[phase22Input, phase22Output]("report").
		Describe("region report").
		Handler(phase22Handler)
	if err := report.Register(srv); err != nil {
		t.Fatalf("register report tool: %v", err)
	}
	// A real MCP App registered via runtime/apps — the inspector previews it.
	if err := apps.Register(srv, apps.App{
		URI:  "ui://phase22/app",
		Name: "phase22-app",
		HTML: []byte(phase22AppHTML),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "phase22-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The real inspector backend, with a real Relay pointed at the SSE sink.
	relay := inspector.NewRelay("http://" + sink.Addr() + "/obs/v1/stream")
	t.Cleanup(func() { _ = relay.Close() })
	go relay.Run(ctx)

	insp, err := inspector.New(inspector.Options{
		Relay: relay,
		ServerInfo: inspector.ServerInfo{
			Name: "phase22-server", Version: "0.1.0", Transport: "inmem",
		},
	})
	if err != nil {
		t.Fatalf("inspector.New: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
	go func() { _ = insp.Serve(ctx) }()

	// A UI client connects to the inspector's obs relay stream.
	streamReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet, insp.URL()+"/api/obs/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	streamResp, err := waitConnect(t, streamReq)
	if err != nil {
		t.Fatalf("connect inspector obs relay: %v", err)
	}
	t.Cleanup(func() { _ = streamResp.Body.Close() })

	// Give the relay a moment to register upstream with the SSE sink, then make
	// a real tool call so the server emits real obs/v1 events.
	waitFor(t, func() bool { return sink.Subscribers() > 0 }, "relay subscribes to sink")
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "emea"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	// The real tool.call obs/v1 event must reach the inspector relay's UI client.
	if !scanForEvent(t, streamResp, `"kind":"tool.call"`) {
		t.Fatal("inspector obs relay did not deliver a tool.call obs/v1 event")
	}

	// The App resource is served by the server — the inspector can preview it.
	readResp, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "ui://phase22/app",
	})
	if err != nil {
		t.Fatalf("ReadResource ui://phase22/app: %v", err)
	}
	if len(readResp.Contents) == 0 ||
		!strings.Contains(readResp.Contents[0].Text, "phase-22 app") {
		t.Fatalf("App resource body unexpected: %+v", readResp.Contents)
	}
}

// TestPhase22_InspectorRefusesNonLoopback is the binding RFC §12 acceptance
// criterion and the ≥1 failure mode: the inspector refuses any non-loopback
// bind — the listener is never opened.
func TestPhase22_InspectorRefusesNonLoopback(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"0.0.0.0:0", "192.168.1.10:0", ":0", "8.8.8.8:0",
	} {
		insp, err := inspector.New(inspector.Options{Addr: addr})
		if err == nil {
			_ = insp.Close()
			t.Fatalf("inspector.New(%q): want refusal, got nil", addr)
		}
		if !errors.Is(err, inspector.ErrNonLoopbackBind) {
			t.Fatalf("inspector.New(%q): want ErrNonLoopbackBind, got %v", addr, err)
		}
	}
	// A loopback bind is accepted — the inspector still serves on localhost.
	insp, err := inspector.New(inspector.Options{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("inspector.New(loopback): %v", err)
	}
	_ = insp.Close()
}

/* --- helpers --------------------------------------------------------- */

// waitConnect retries an HTTP request until the server accepts it.
func waitConnect(t *testing.T, req *http.Request) (*http.Response, error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.DefaultClient.Do(req.Clone(req.Context()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return nil, lastErr
}

// waitFor polls cond until it is true or a deadline elapses.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// scanForEvent reads the inspector relay SSE body until a frame contains needle.
func scanForEvent(t *testing.T, resp *http.Response, needle string) bool {
	t.Helper()
	found := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.Contains(sc.Text(), needle) {
				found <- true
				return
			}
		}
		found <- false
	}()
	select {
	case ok := <-found:
		return ok
	case <-time.After(4 * time.Second):
		return false
	}
}
