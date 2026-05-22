// This file is the Phase 23 cross-subsystem integration test (AGENTS.md §17).
// Phase 23's Deps name shipped phases — 22 (the inspector core), 14
// (runtime/tasks), 21 (the internal/validate quality gate) — and it consumes
// the codegen contracts. The test drives the real seam end-to-end:
//
//   - a real runtime/server + runtime/apps App + runtime/obs SSE sink, with the
//     standalone `dockyard inspect`-style inspector backend attached to the
//     running server's obs stream — asserts `dockyard inspect` attaches to a
//     running server (the binding RFC §12 acceptance criterion);
//   - a real tasks.Engine driving a task to a terminal status — asserts the
//     inspector relays the real task.progress obs/v1 events the Tasks panel
//     renders;
//   - a real internal/validate.Run behind inspector.VerdictsFromValidate —
//     asserts the Verdicts endpoint surfaces a real validate result;
//   - the inspector refuses a non-loopback bind — the ≥1 failure mode.
//
// No mock at any seam: a real server, App, SSE sink, tasks engine, validate
// run, and the real inspector backend. Runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/inspector"
	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// phase23Input / phase23Output are the integration tool contract.
type phase23Input struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type phase23Output struct {
	Region string `json:"region"`
	Total  int    `json:"total"`
}

func phase23Handler(_ context.Context, in phase23Input) (tool.Result[phase23Output], error) {
	return tool.Result[phase23Output]{
		Text:       "region " + in.Region + " reported",
		Structured: phase23Output{Region: in.Region, Total: 11},
	}, nil
}

const phase23AppHTML = `<!doctype html><html><head><title>Phase 23 App</title>` +
	`</head><body><div id="app">phase-23 app</div></body></html>`

// TestPhase23_InspectAttachesToRunningServer wires a real server + App + SSE
// sink + tasks engine and drives the inspector backend exactly as `dockyard
// inspect --url` does — the inspector serving on a loopback port, relaying the
// running server's obs/v1 stream. It asserts the inspector attaches, a real
// task lifecycle is relayed, and the Verdicts endpoint surfaces a real result.
func TestPhase23_InspectAttachesToRunningServer(t *testing.T) {
	t.Parallel()

	// A real obs SSE sink behind the emitter seam — the inspector's obs source.
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// A real server emitting obs/v1 to the sink.
	srv, err := server.New(
		server.Info{Name: "phase23-server", Version: "0.2.0"},
		&server.Options{Obs: sink, Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[phase23Input, phase23Output]("report").
		Describe("region report").
		Handler(phase23Handler)
	if err := report.Register(srv); err != nil {
		t.Fatalf("register report tool: %v", err)
	}
	if err := apps.Register(srv, apps.App{
		URI:  "ui://phase23/app",
		Name: "phase23-app",
		HTML: []byte(phase23AppHTML),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "phase23-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The inspector backend, attached to the running server's obs stream — the
	// `dockyard inspect --url` configuration, with a verdict source.
	relay := inspector.NewRelay("http://" + sink.Addr() + "/obs/v1/stream")
	t.Cleanup(func() { _ = relay.Close() })
	go relay.Run(ctx)

	insp, err := inspector.New(inspector.Options{
		Addr:  "127.0.0.1:0",
		Relay: relay,
		ServerInfo: inspector.ServerInfo{
			Name: "phase23-server", Version: "0.2.0", Transport: "http",
		},
		// A real verdict source — internal/validate.Run rooted at a directory
		// with no manifest yields a real Blocker verdict (the failure surfaces
		// as a verdict, never a void).
		Verdicts: inspector.VerdictsFromValidate(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("inspector.New: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
	go func() { _ = insp.Serve(ctx) }()

	// A UI client connects to the inspector's obs relay — proving the inspector
	// has attached to the running server.
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
	waitFor(t, func() bool { return sink.Subscribers() > 0 }, "relay subscribes to sink")

	// A real tool call — the inspector relays its tool.call obs/v1 event.
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "emea"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !scanForEvent(t, streamResp, `"kind":"tool.call"`) {
		t.Fatal("inspector did not relay a tool.call obs/v1 event from the running server")
	}

	// A real task lifecycle — a real tasks.Engine emitting to the same SSE
	// sink. The inspector must relay the task.progress obs/v1 events the Tasks
	// panel renders.
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		Logger:   quietLogger(),
		Obs:      sink, // same obs/v1 stream the inspector relays
		ServerID: "phase23-server",
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	taskDone := make(chan struct{})
	if _, err := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "report",
		TaskMeta: protocolcodec.TaskMeta{TTL: ptrInt64(60000)},
		Run: func(context.Context) (json.RawMessage, error) {
			defer close(taskDone)
			return json.RawMessage(`{"ok":true}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	<-taskDone
	if !scanForEvent(t, streamResp, `"kind":"task.progress"`) {
		t.Fatal("inspector did not relay a task.progress obs/v1 event")
	}

	// The Verdicts endpoint surfaces a real internal/validate result.
	verdictsResp, err := http.Get(insp.URL() + "/api/verdicts") //nolint:noctx // test
	if err != nil {
		t.Fatalf("GET /api/verdicts: %v", err)
	}
	defer func() { _ = verdictsResp.Body.Close() }()
	var verdicts []inspector.Verdict
	if err := json.NewDecoder(verdictsResp.Body).Decode(&verdicts); err != nil {
		t.Fatalf("decode verdicts: %v", err)
	}
	if len(verdicts) == 0 {
		t.Fatal("Verdicts endpoint returned no verdicts for a real validate run")
	}
	// A project dir with no manifest yields a real manifest blocker verdict.
	sawError := false
	for _, v := range verdicts {
		if v.Severity == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected a real validate blocker verdict, got: %+v", verdicts)
	}

	// The App resource is served by the running server — the inspector previews it.
	readResp, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "ui://phase23/app",
	})
	if err != nil {
		t.Fatalf("ReadResource ui://phase23/app: %v", err)
	}
	if len(readResp.Contents) == 0 ||
		!strings.Contains(readResp.Contents[0].Text, "phase-23 app") {
		t.Fatalf("App resource body unexpected: %+v", readResp.Contents)
	}
}

// TestPhase23_InspectRefusesNonLoopback is the ≥1 failure mode: `dockyard
// inspect` cannot widen the inspector off-localhost — a non-loopback bind is
// refused before the listener opens (RFC §12, the CVE-2025-49596 lesson).
func TestPhase23_InspectRefusesNonLoopback(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.10:0", "8.8.8.8:0"} {
		insp, err := inspector.New(inspector.Options{Addr: addr})
		if err == nil {
			_ = insp.Close()
			t.Fatalf("inspector.New(%q): want refusal, got nil", addr)
		}
		if !errors.Is(err, inspector.ErrNonLoopbackBind) {
			t.Fatalf("inspector.New(%q): want ErrNonLoopbackBind, got %v", addr, err)
		}
	}
}
