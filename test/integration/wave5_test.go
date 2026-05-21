// This file is the Wave 5 wave-end end-to-end integration test (CLAUDE.md §17 /
// §17.5 — the wave-boundary checkpoint). Wave 5 shipped the MCP Tasks extension
// (RFC §8): runtime/tasks ships the server-side Tasks engine — the tasks/*
// router, the five-status lifecycle, the CreateTaskResult substitution for a
// task-augmented tools/call, and the capability-driven `tasks` advertisement
// (Phase 13); the durable TaskStore facade over the modernc.org/sqlite Store
// seam with its own forward-only migration and shared conformance suite, the
// TaskHandle handler API (progress, status, cooperative cancellation,
// input_required elicitation), the manifest-tunable lifecycle controls (max
// TTL, per-requestor concurrency cap, background TTL purge sweep), the task
// security model (crypto-strong IDs, auth-context binding, tasks/list
// withholding), and the tasks/* transport mount that routes JSON-RPC frames
// into Engine.DispatchAs ahead of the SDK server and injects the
// `capabilities.tasks` block into the initialize handshake (Phase 14).
//
// This test drives the integrated Wave 5 surface end to end with REAL
// components and no mocks at the seams: a contract-first task-augmented tool is
// registered on a real runtime/server, served over a real streamable-HTTP
// transport behind an httptest.Server with the real tasks.Mount middleware in
// front; a real durable TaskStore over a real modernc.org/sqlite Store backs
// the engine; a real SDK client performs the initialize handshake (and reads
// the injected `capabilities.tasks` block), and a raw MCP/JSON-RPC client
// drives the full tasks/* lifecycle over the same wire — a task-augmented
// tools/call returns CreateTaskResult, then tasks/get → tasks/result →
// tasks/list → tasks/cancel behave per the vendored spec; a long handler
// reports progress through a TaskHandle and is cooperatively cancelled; an
// input_required round-trip completes. It covers ≥1 failure mode per seam (an
// illegal lifecycle transition, a cross-context access rejected without leaking
// existence, tasks/list withheld in the unauthenticated path, TTL expiry + the
// purge sweep reaping a task), proves auth-context / capability propagation
// from the transport through DispatchAs to the durable TaskStore, and runs an
// N>=10 concurrency stress under -race against the shared engine, durable
// TaskStore and purge sweep with a post-teardown goroutine-leak assertion.
//
// The Wave 5 surface is the Tasks extension as one wired whole; it does not
// re-prove the Wave 3/4 server-core or Apps pipeline. Shared helpers —
// quietLogger, stableGoroutineCount, assertNoGoroutineLeak — are defined once
// for the integration package in wave1_test.go and reused here. The codec and
// taskIDParams helpers are defined in phase13_tasks_test.go. See decision
// D-072.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ---- the Wave 5 contract -----------------------------------------------------
//
// wave5ReportInput / wave5ReportOutput is the contract-first task-augmented tool: a typed
// input the generated input schema constrains and a typed output. The tool is a
// genuine long-running unit of work, so it is created as a Task rather than run
// synchronously — exactly the task-augmented tools/call shape (RFC §8.3).

type wave5ReportInput struct {
	// Account is the account a report is generated for — a required field of
	// the generated input schema.
	Account string `json:"account" jsonschema:"the account to report on"`
}

type wave5ReportOutput struct {
	Account string `json:"account"`
	Lines   int    `json:"lines"`
}

// ---- shared Wave 5 fixtures --------------------------------------------------

// migrationSetupMu serializes the global Store migration-registry mutation in
// newWave5Engine. tasks.RegisterMigrations / store.ResetMigrationsForTest mutate
// process-global state (store.AddMigration panics on a duplicate ID), so the
// reset → register → Migrate sequence of one engine must complete before
// another parallel test's begins. Holding it across Migrate is sufficient: once
// a Store has run its migrations the registry can be reset for the next test.
var migrationSetupMu sync.Mutex

// newWave5Engine builds a real tasks.Engine over the real durable TaskStore
// layered on a real modernc.org/sqlite Store — the V1 durable backing, no mocks
// at the Store seam. It returns the engine and the backing Store; the caller
// closes the Store. The global migration-registry mutation is serialized so the
// fixture is safe under t.Parallel().
func newWave5Engine(t *testing.T, opts *tasks.Options) (*tasks.Engine, store.Store) {
	t.Helper()

	st, err := sqlitestore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("sqlitestore.Open: %v", err)
	}
	migrationSetupMu.Lock()
	store.ResetMigrationsForTest()
	tasks.RegisterMigrations()
	migrateErr := st.Migrate(context.Background())
	store.ResetMigrationsForTest()
	migrationSetupMu.Unlock()
	if migrateErr != nil {
		t.Fatalf("Store.Migrate: %v", migrateErr)
	}

	ts, err := tasks.NewStore(st)
	if err != nil {
		t.Fatalf("tasks.NewStore: %v", err)
	}
	if opts == nil {
		opts = &tasks.Options{}
	}
	e, err := tasks.NewEngine(ts, opts)
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	return e, st
}

// newWave5Server builds a real runtime/server carrying the contract-first
// wave5ReportInput→wave5ReportOutput tool — the server half of the wired transport. The
// Tasks mount sits in front of this server's HTTP handler; the SDK server still
// answers initialize and tools/list, the mount answers tasks/*.
func newWave5Server(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.New(server.Info{
		Name:    "wave5-app",
		Title:   "Wave 5 Tasks App",
		Version: "5.0.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	reportTool := tool.New[wave5ReportInput, wave5ReportOutput]("generate_report").
		Describe("generate a long-running account report").
		Handler(func(_ context.Context, in wave5ReportInput) (tool.Result[wave5ReportOutput], error) {
			return tool.Result[wave5ReportOutput]{
				Text:       "Report generated for " + in.Account + ".",
				Structured: wave5ReportOutput{Account: in.Account, Lines: 42},
			}, nil
		})
	if err := reportTool.Register(s); err != nil {
		t.Fatalf("Register generate_report: %v", err)
	}
	return s
}

// wave5HTTP wires the integrated transport: the real runtime/server HTTP
// handler with the real tasks.Mount middleware in front, served behind an
// httptest.Server. authFn supplies the requestor's auth context to the mount
// (nil = the unauthenticated path). It returns the live httptest.Server.
func wave5HTTP(t *testing.T, s *server.Server, e *tasks.Engine, authFn tasks.AuthContextFunc) *httptest.Server {
	t.Helper()
	sdkHandler, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("server.HTTPHandler: %v", err)
	}
	mount := tasks.NewMount(e)
	if authFn != nil {
		mount = mount.WithAuthContext(authFn)
	}
	ts := httptest.NewServer(mount.HTTPMiddleware(sdkHandler))
	t.Cleanup(ts.Close)
	return ts
}

// rpcPost sends one JSON-RPC request frame to the wired endpoint and decodes
// the response envelope. It is a raw MCP/JSON-RPC client — the tasks/* frames
// are answered by the mount, not the SDK server.
func rpcPost(t *testing.T, url, method string, params json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		frame["params"] = params
	}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal %s frame: %v", method, err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s build request: %v", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The streamable-HTTP transport requires this Accept header; a tasks/* frame
	// is intercepted by the mount before the SDK sees it, so setting it is
	// harmless there and required for a frame (initialize) the mount forwards.
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s POST: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s read body: %v", method, err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("%s decode response: %v (body %s)", method, err, out)
	}
	return decoded
}

// postInitializeCapabilities sends a raw initialize frame to the wired endpoint
// and returns the `result.capabilities` object. The real go-sdk streamable-HTTP
// transport frames the initialize response as SSE (text/event-stream), so this
// decodes the JSON-RPC envelope from the SSE `data:` line as well as a
// plain-JSON body — proving the mount's capability injection works over the
// real transport, not only a plain-JSON stand-in.
func postInitializeCapabilities(t *testing.T, url string) map[string]json.RawMessage {
	t.Helper()
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "wave5-raw", "version": "0"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(frame))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)

	// Extract the JSON-RPC envelope: a plain body is the envelope itself; an
	// SSE body carries it on a `data:` line.
	envelopeJSON := bytes.TrimSpace(out)
	if !bytes.HasPrefix(envelopeJSON, []byte("{")) {
		for _, line := range bytes.Split(out, []byte("\n")) {
			if d := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))); len(d) > 0 && d[0] == '{' {
				envelopeJSON = d
				break
			}
		}
	}
	var envelope struct {
		Result struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(envelopeJSON, &envelope); err != nil {
		t.Fatalf("decode initialize envelope: %v (body %s)", err, out)
	}
	return envelope.Result.Capabilities
}

// ---- happy path: the full Tasks lifecycle over a real transport -------------

// TestWave5_TaskLifecycleOverRealTransport drives the integrated Wave 5 surface
// end to end: a real SDK client performs the initialize handshake against the
// real runtime/server behind the real tasks.Mount and reads the injected
// `capabilities.tasks` block; then a raw JSON-RPC client drives the full
// tasks/* lifecycle (CreateTaskResult is produced server-side, then tasks/get →
// tasks/result → tasks/list → tasks/cancel) over the same wire, against a real
// durable TaskStore over real sqlite.
func TestWave5_TaskLifecycleOverRealTransport(t *testing.T) {
	t.Parallel()
	e, st := newWave5Engine(t, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	defer func() { _ = st.Close() }()
	s := newWave5Server(t)
	ts := wave5HTTP(t, s, e, nil)

	// 1. A real SDK client completes the initialize handshake over the real
	//    streamable-HTTP transport — the mount captures the SDK's initialize
	//    response and merges in the `capabilities.tasks` block the SDK has no
	//    native field for. The handshake must still succeed and expose tools.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave5-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("SDK client connect over streamable-HTTP: %v", err)
	}
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list over the wired transport: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "generate_report" {
		t.Fatalf("tools/list = %v, want [generate_report] — the SDK server still answers behind the mount", tools.Tools)
	}

	// The raw initialize frame proves the `capabilities.tasks` block is injected
	// — the capability the SDK handshake cannot carry natively. The real go-sdk
	// streamable-HTTP transport frames the initialize response as SSE
	// (text/event-stream), so the mount must merge the capability into the SSE
	// `data:` payload, not only a plain-JSON body (D-072).
	initResult := postInitializeCapabilities(t, ts.URL)
	if _, ok := initResult["tasks"]; !ok {
		t.Fatal("initialize handshake over the real (SSE-framed) transport carries no capabilities.tasks block")
	}
	if _, ok := initResult["tools"]; !ok {
		t.Fatal("the SDK's own capabilities were dropped during tasks-capability injection")
	}

	// 2. A task-augmented tools/call: the engine substitutes a CreateTaskResult
	//    (status working) for the immediate tool result. The real durable
	//    sqlite store records the task.
	release := make(chan struct{})
	want := json.RawMessage(`{"content":[{"type":"text","text":"report ready"}],"isError":false}`)
	raw, err := e.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "generate_report",
		TaskMeta: protocolcodec.TaskMeta{TTL: ptrInt64(60000)},
		Run: func(rc context.Context) (json.RawMessage, error) {
			select {
			case <-release:
			case <-rc.Done():
				return nil, rc.Err()
			}
			return want, nil
		},
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := codec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("DecodeCreateTaskResult: %v", err)
	}
	if created.Task.Status != protocolcodec.TaskWorking {
		t.Fatalf("CreateTaskResult status = %q, want working", created.Task.Status)
	}
	id := created.Task.ID

	// 3. tasks/get over the wire is non-blocking — `working` while the task runs.
	getResp := rpcPost(t, ts.URL, tasks.MethodGet, taskIDParams(t, id))
	if errRaw, isErr := getResp["error"]; isErr {
		t.Fatalf("tasks/get over the transport errored: %s", errRaw)
	}
	polled, err := codec.DecodeGetTaskResult(getResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/get result: %v", err)
	}
	if polled.ID != id || polled.Status != protocolcodec.TaskWorking {
		t.Fatalf("tasks/get over the wire = (%q,%q), want (%q,working)", polled.ID, polled.Status, id)
	}

	// 4. tasks/result over the wire blocks until terminal — release the work,
	//    then collect the underlying CallToolResult verbatim, with the
	//    related-task _meta key stamped on it.
	resultCh := make(chan map[string]json.RawMessage, 1)
	go func() { resultCh <- rpcPost(t, ts.URL, tasks.MethodResult, taskIDParams(t, id)) }()
	close(release)
	select {
	case resultResp := <-resultCh:
		if errRaw, isErr := resultResp["error"]; isErr {
			t.Fatalf("tasks/result over the transport errored: %s", errRaw)
		}
		var obj map[string]any
		if err := json.Unmarshal(resultResp["result"], &obj); err != nil {
			t.Fatalf("tasks/result not a JSON object: %v", err)
		}
		if obj["isError"] != false {
			t.Fatalf("tasks/result lost the underlying CallToolResult: %#v", obj)
		}
		meta, _ := obj["_meta"].(map[string]any)
		rel, _ := meta["io.modelcontextprotocol/related-task"].(map[string]any)
		if rel["taskId"] != id {
			t.Fatalf("tasks/result over the wire missing related-task _meta: %#v", obj)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tasks/result over the transport did not return")
	}

	// 5. tasks/list over the wire — the now-terminal task is listed.
	listResp := rpcPost(t, ts.URL, tasks.MethodList, nil)
	if errRaw, isErr := listResp["error"]; isErr {
		t.Fatalf("tasks/list over the transport errored: %s", errRaw)
	}
	list, err := codec.DecodeListTasksResult(listResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/list result: %v", err)
	}
	if len(list.Tasks) != 1 || list.Tasks[0].ID != id {
		t.Fatalf("tasks/list over the wire = %v, want [%s]", list.Tasks, id)
	}

	// 6. tasks/cancel over the wire on a now-terminal task — the spec mandates
	//    -32602, surfaced as a JSON-RPC error frame (failure mode on the mount).
	cancelResp := rpcPost(t, ts.URL, tasks.MethodCancel, taskIDParams(t, id))
	if _, isErr := cancelResp["error"]; !isErr {
		t.Fatal("tasks/cancel of a terminal task over the transport must be a JSON-RPC error")
	}
}

// ---- TaskHandle: progress + cooperative cancellation ------------------------

// TestWave5_TaskHandleProgressAndCancel proves a long-running handler reports
// progress through a real TaskHandle and is cooperatively cancelled: tasks/cancel
// cancels the handler's context, the handler observes it through Cancelled() and
// ctx.Done() and unwinds, and the task ends `cancelled`.
func TestWave5_TaskHandleProgressAndCancel(t *testing.T) {
	t.Parallel()
	e, st := newWave5Engine(t, &tasks.Options{PollInterval: 10})
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	progressed := make(chan struct{}, 1)
	unwound := make(chan struct{})
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "generate_report",
		Handle: func(hc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			if err := h.Progress(hc, 0.25, "gathering rows"); err != nil {
				return nil, err
			}
			select {
			case progressed <- struct{}{}:
			default:
			}
			// Block until cooperatively cancelled.
			for {
				if h.Cancelled() {
					close(unwound)
					return nil, errors.New("handler unwound after cancellation")
				}
				select {
				case <-hc.Done():
					close(unwound)
					return nil, hc.Err()
				case <-time.After(5 * time.Millisecond):
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := codec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Task.ID

	// Wait for the handler to report progress, then assert tasks/get observes
	// the refreshed status message — progress without a lifecycle move.
	select {
	case <-progressed:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never reported progress")
	}
	getRaw, err := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	polled, err := codec.DecodeGetTaskResult(getRaw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if polled.Status != protocolcodec.TaskWorking {
		t.Fatalf("status after Progress = %q, want working (progress is not a transition)", polled.Status)
	}
	if polled.StatusMessage == "" {
		t.Fatal("TaskHandle.Progress did not refresh the task status message")
	}

	// tasks/cancel cooperatively cancels the handler.
	cancelRaw, err := e.Dispatch(ctx, tasks.MethodCancel, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	cancelled, err := codec.DecodeGetTaskResult(cancelRaw)
	if err != nil {
		t.Fatalf("decode cancel result: %v", err)
	}
	if cancelled.Status != protocolcodec.TaskCancelled {
		t.Fatalf("tasks/cancel left status %q, want cancelled", cancelled.Status)
	}
	select {
	case <-unwound:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not observe cooperative cancellation and unwind")
	}
}

// ---- TaskHandle: input_required round-trip ----------------------------------

// TestWave5_InputRequiredRoundTrip drives the input_required elicitation: a
// handler raises a prompt via RequireInput, the task moves to input_required,
// the driver supplies the reply through Engine.SupplyInput, and the handler
// resumes and completes — the task ends `completed`.
func TestWave5_InputRequiredRoundTrip(t *testing.T) {
	t.Parallel()
	e, st := newWave5Engine(t, &tasks.Options{PollInterval: 10})
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "generate_report",
		Handle: func(hc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			resp, err := h.RequireInput(hc, tasks.InputPrompt{Message: "which fiscal quarter?"})
			if err != nil {
				return nil, err
			}
			if resp.Declined {
				return nil, errors.New("requestor declined input")
			}
			return json.RawMessage(fmt.Sprintf(`{"isError":false,"quarter":%s}`, resp.Data)), nil
		},
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	// Wait for the handler to raise the input_required prompt.
	var prompt tasks.InputPrompt
	deadline := time.After(3 * time.Second)
	for {
		p, ok := e.PendingInput(id)
		if ok {
			prompt = p
			break
		}
		select {
		case <-deadline:
			t.Fatal("handler never raised an input_required elicitation")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if prompt.Message != "which fiscal quarter?" {
		t.Fatalf("elicitation prompt = %q, want the handler's prompt", prompt.Message)
	}
	// The task is in input_required while the handler waits.
	getRaw, _ := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
	mid, _ := codec.DecodeGetTaskResult(getRaw)
	if mid.Status != protocolcodec.TaskInputRequired {
		t.Fatalf("status while awaiting input = %q, want input_required", mid.Status)
	}

	// Supply the reply; the handler resumes and the task completes.
	if err := e.SupplyInput(ctx, id, tasks.InputResponse{Data: []byte(`"Q3"`)}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	resultRaw, err := e.Dispatch(ctx, tasks.MethodResult, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/result after input: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(resultRaw, &obj); err != nil {
		t.Fatalf("tasks/result decode: %v", err)
	}
	if obj["quarter"] != "Q3" {
		t.Fatalf("handler did not consume the supplied input: %#v", obj)
	}
}

// ---- failure mode: illegal lifecycle transition -----------------------------

// TestWave5_FailureMode_IllegalTransition is a mandated failure mode on the
// durable TaskStore seam: an illegal lifecycle transition through the real
// sqlite-backed store is a typed error, never a panic across the boundary.
func TestWave5_FailureMode_IllegalTransition(t *testing.T) {
	t.Parallel()
	_, st := newWave5Engine(t, nil)
	defer func() { _ = st.Close() }()
	ts, err := tasks.NewStore(st)
	if err != nil {
		t.Fatalf("tasks.NewStore: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if err := ts.Create(ctx, tasks.TaskRecord{
		ID:        "task_wave5illegal0000000000000000",
		Status:    protocolcodec.TaskWorking,
		CreatedAt: now,
		UpdatedAt: now,
		Method:    "tools/call",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := ts.Transition(ctx, "task_wave5illegal0000000000000000", protocolcodec.TaskCompleted, "done"); err != nil {
		t.Fatalf("legal working→completed errored: %v", err)
	}
	// completed → working is illegal — a terminal status is immutable.
	_, err = ts.Transition(ctx, "task_wave5illegal0000000000000000", protocolcodec.TaskWorking, "")
	if !errors.Is(err, tasks.ErrIllegalTransition) {
		t.Fatalf("completed→working over the durable store: want ErrIllegalTransition, got %v", err)
	}
}

// ---- failure mode + identity propagation: cross-context rejection -----------

// TestWave5_CrossContextRejectionOverTransport proves auth-context propagation
// end to end: the auth context flows from the transport (the mount's
// AuthContextFunc) through DispatchAs to the durable TaskStore and gates access.
// A task created under one context is not reachable from another, and the
// rejection does not leak the task's existence — it is reported exactly as a
// missing task (failure mode on the auth seam).
func TestWave5_CrossContextRejectionOverTransport(t *testing.T) {
	t.Parallel()
	e, st := newWave5Engine(t, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	defer func() { _ = st.Close() }()
	s := newWave5Server(t)

	// The mount derives the requestor's auth context from an X-Auth header —
	// the deployment-supplied identity seam.
	authFn := func(r *http.Request) string { return r.Header.Get("X-Auth") }
	ts := wave5HTTP(t, s, e, authFn)

	// Alice creates a task; her auth context is recorded on the durable record.
	raw, err := e.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName:    "generate_report",
		AuthContext: "alice",
		Run:         func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"isError":false}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	postAs := func(auth, method string, params json.RawMessage) map[string]json.RawMessage {
		t.Helper()
		frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
		body, _ := json.Marshal(frame)
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("X-Auth", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s POST as %q: %v", method, auth, err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatalf("%s decode: %v (body %s)", method, err, out)
		}
		return decoded
	}

	// Alice reaches her own task over the wire.
	aliceResp := postAs("alice", tasks.MethodGet, taskIDParams(t, id))
	if errRaw, isErr := aliceResp["error"]; isErr {
		t.Fatalf("alice's own tasks/get over the transport errored: %s", errRaw)
	}

	// Bob is rejected — the auth context propagated from the header through the
	// mount and DispatchAs to the durable store. The error is -32602 and reads
	// as "task not found": Bob cannot tell a cross-context task from a missing
	// one (no existence leak).
	bobResp := postAs("bob", tasks.MethodGet, taskIDParams(t, id))
	var bobErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if errRaw, isErr := bobResp["error"]; !isErr {
		t.Fatal("bob's cross-context tasks/get over the transport must be a JSON-RPC error")
	} else if err := json.Unmarshal(errRaw, &bobErr); err != nil {
		t.Fatalf("decode bob's error: %v", err)
	}
	if bobErr.Code != tasks.CodeInvalidParams {
		t.Fatalf("cross-context rejection code = %d, want %d", bobErr.Code, tasks.CodeInvalidParams)
	}
	if !bytes.Contains([]byte(bobErr.Message), []byte("task not found")) {
		t.Fatalf("cross-context rejection message %q leaks the task's existence — must read as 'task not found'", bobErr.Message)
	}
}

// ---- failure mode: tasks/list withheld in the unauthenticated path ----------

// TestWave5_TasksListWithheldUnauthenticated proves tasks/list is withheld —
// not advertised and not served — when the engine cannot identify requestors
// (the unauthenticated single-user path; RFC §8.5). It is a mandated failure
// mode on the capability seam.
func TestWave5_TasksListWithheldUnauthenticated(t *testing.T) {
	t.Parallel()
	// AdvertiseList is opted on but RequestorIdentifiable is off — the engine
	// must STILL withhold tasks/list.
	e, st := newWave5Engine(t, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: false,
		PollInterval:          10,
	})
	defer func() { _ = st.Close() }()

	// The capability block does not advertise list.
	capRaw, err := e.CapabilityJSON()
	if err != nil {
		t.Fatalf("CapabilityJSON: %v", err)
	}
	capBlock, ok, err := codec.DecodeTasksServerCapability(capRaw)
	if err != nil {
		t.Fatalf("decode capability: %v", err)
	}
	if !ok {
		t.Fatal("CapabilityJSON produced no decodable capabilities.tasks block")
	}
	if capBlock.List {
		t.Fatal("tasks capability advertises list despite an unidentifiable-requestor engine")
	}

	// And the method is not served — Dispatch rejects it as unknown.
	_, err = e.Dispatch(context.Background(), tasks.MethodList, nil)
	if !errors.Is(err, tasks.ErrUnknownMethod) {
		t.Fatalf("tasks/list in the unauthenticated path: want ErrUnknownMethod, got %v", err)
	}
}

// ---- failure mode: TTL expiry + the purge sweep -----------------------------

// TestWave5_TTLPurgeSweepReapsExpired is a mandated failure mode on the
// lifecycle seam: an expired task is reaped by the background TTL purge sweep
// from the real durable sqlite store.
func TestWave5_TTLPurgeSweepReapsExpired(t *testing.T) {
	t.Parallel()
	e, st := newWave5Engine(t, &tasks.Options{
		PollInterval: 10,
		Lifecycle:    tasks.LifecycleFromMillis(20, 0, 5, 0), // 20ms max TTL, 5ms purge cadence
	})
	defer func() { _ = st.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ttl := int64(10) // 10ms — well inside the sweep's reach
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "generate_report",
		TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"isError":false}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	e.StartSweep(ctx)
	defer e.StopSweep()

	deadline := time.After(3 * time.Second)
	for {
		_, err := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
		if errors.Is(err, tasks.ErrTaskNotFound) {
			return // reaped from the durable store — success
		}
		select {
		case <-deadline:
			t.Fatal("the TTL purge sweep did not reap the expired task from the durable store")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ---- concurrency stress: N>=10 under -race ----------------------------------

// TestWave5_ConcurrencyStress drives an N>=10 concurrency stress against the
// shared reusable artifacts — the engine, the durable TaskStore over real
// sqlite, and the purge sweep racing live tasks — under -race, with a
// post-teardown goroutine-leak assertion (CLAUDE.md §17). Each worker is its
// own auth context, so the auth-scoped listing races concurrent creates and the
// purge sweep on every iteration.
func TestWave5_ConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	e, st := newWave5Engine(t, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          5,
		Lifecycle: tasks.LifecycleFromMillis(
			40, // max TTL 40ms
			0,  // no default TTL
			3,  // purge every 3ms — races the live tasks
			64, // generous per-requestor cap
		),
	})
	ctx, cancel := context.WithCancel(context.Background())
	e.StartSweep(ctx)

	const workers = 12
	const perWorker = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			authCtx := fmt.Sprintf("ctx-%d", w)
			for i := 0; i < perWorker; i++ {
				ttl := int64(20) // short — the purge sweep races these
				raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
					ToolName:    "generate_report",
					AuthContext: authCtx,
					TaskMeta:    protocolcodec.TaskMeta{TTL: &ttl},
					Run:         func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"isError":false}`), nil },
				})
				if err != nil {
					t.Errorf("worker %d CreateForToolCall: %v", w, err)
					return
				}
				created, err := codec.DecodeCreateTaskResult(raw)
				if err != nil {
					t.Errorf("worker %d decode: %v", w, err)
					return
				}
				// A concurrent auth-scoped tasks/get and tasks/list race the
				// other workers' creates and the purge sweep — neither must
				// error spuriously, leak a context, or panic.
				_, gerr := e.DispatchAs(ctx, authCtx, tasks.MethodGet, taskIDParams(t, created.Task.ID))
				if gerr != nil && !errors.Is(gerr, tasks.ErrTaskNotFound) {
					t.Errorf("worker %d concurrent tasks/get: %v", w, gerr)
					return
				}
				if _, lerr := e.DispatchAs(ctx, authCtx, tasks.MethodList, nil); lerr != nil {
					t.Errorf("worker %d concurrent tasks/list: %v", w, lerr)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// Tear down: stop the sweep, close the durable store, assert no leak.
	cancel()
	e.StopSweep()
	if err := st.Close(); err != nil {
		t.Fatalf("store Close: %v", err)
	}
	assertNoGoroutineLeak(t, baseline)
}
