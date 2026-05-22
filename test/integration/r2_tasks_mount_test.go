// This file is the R2 depth-audit remediation integration test (CLAUDE.md §17).
//
// The finding: the Tasks transport mount (runtime/tasks.Mount) existed and was
// tested in isolation, but nothing joined it to the real server transport —
// runtime/server never referenced tasks.Mount, so a real MCP client could not
// drive tasks/* over a real Dockyard server. The Phase 14 integration test met
// the "tasks/* over a transport" criterion only with a hand-written sdkStandIn,
// not the product.
//
// This test closes that gap with NO mocks at the seam: a real runtime/server
// with a real tasks.Engine attached via the new server seam (server.Options.
// Tasks), served over the REAL streamable-HTTP transport behind an
// httptest.Server, with a REAL go-sdk MCP client completing the initialize
// handshake (and seeing the injected capabilities.tasks block) and issuing a
// task-augmented tools/call. The tasks/* lifecycle — get, result, list, cancel
// — is then driven over the same real server.HTTPHandler; tasks/* are MCP Tasks
// extension methods the go-sdk client cannot send natively (the experimental
// extension is outside the SDK's dispatch table — RFC §8.2), so a real Tasks
// client speaks them as raw JSON-RPC, exactly as this test does.
//
// It covers a failure mode (CLAUDE.md §17): a tasks/cancel of an
// already-terminal task is rejected with the spec's -32602 over the real wire.
// It runs under -race.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// r2ReportInput is the typed input of the task-augmented tool — a contract-first
// Go struct (P1); the go-sdk infers and validates its schema.
type r2ReportInput struct {
	Account string `json:"account" jsonschema:"the account to report on"`
}

// r2ReportOutput is the task-augmented tool's typed output. The tool is a
// long-running unit of work: its handler creates a Task and returns the task's
// identity rather than blocking — exactly the task-augmented tools/call shape
// (RFC §8.3). TaskID is the created task's id; TaskStatus is its initial
// status.
type r2ReportOutput struct {
	TaskID     string `json:"taskId"`
	TaskStatus string `json:"taskStatus"`
}

// TestR2_TasksOverRealServerTransport is the binding R2 integration test: a
// real MCP SDK client drives a task-augmented tools/call over the real
// runtime/server streamable-HTTP transport with a real tasks.Engine attached
// through the new server.Options.Tasks seam, then the full tasks/* lifecycle is
// driven over that same real server.
func TestR2_TasksOverRealServerTransport(t *testing.T) {
	t.Parallel()

	// --- real durable Tasks engine over a real sqlite Store (no mocks) -------
	st, err := sqlitestore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("sqlitestore.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(context.Background(), tasks.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ts, err := tasks.NewStore(st)
	if err != nil {
		t.Fatalf("tasks.NewStore: %v", err)
	}
	engine, err := tasks.NewEngine(ts, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}

	// release gates the task's underlying work so the test can observe the
	// working status before the task completes.
	release := make(chan struct{})
	terminalPayload := json.RawMessage(
		`{"content":[{"type":"text","text":"report ready"}],"isError":false}`)

	// --- a real runtime/server with the Tasks engine attached via the seam ---
	// This is the R2 fix under test: server.Options.Tasks joins tasks.Mount to
	// the server; the test does not wrap the handler with the mount itself.
	srv, err := server.New(server.Info{Name: "r2-tasks", Version: "0.1.0"},
		&server.Options{Tasks: engine})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if !srv.TasksEnabled() {
		t.Fatal("server.Options.Tasks did not attach a Tasks engine to the server")
	}

	// A contract-first task-augmented tool: its handler creates a Task and
	// returns the task identity. A real SDK client issues this tools/call.
	report := func(ctx context.Context, _ r2ReportInput) (r2ReportOutput, error) {
		raw, cerr := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
			ToolName: "generate_report",
			Run: func(rc context.Context) (json.RawMessage, error) {
				select {
				case <-release:
				case <-rc.Done():
					return nil, rc.Err()
				}
				return terminalPayload, nil
			},
		})
		if cerr != nil {
			return r2ReportOutput{}, cerr
		}
		created, derr := codec.DecodeCreateTaskResult(raw)
		if derr != nil {
			return r2ReportOutput{}, derr
		}
		return r2ReportOutput{
			TaskID:     created.Task.ID,
			TaskStatus: string(created.Task.Status),
		}, nil
	}
	if err := server.AddTool(srv,
		server.ToolDef{Name: "generate_report", Description: "generate an account report as a task"},
		report); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	// The real streamable-HTTP transport, with the secure default posture. The
	// mount is wired inside HTTPHandler — the test does NOT add it.
	handler, err := srv.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// --- a REAL go-sdk MCP client over the real streamable-HTTP transport ----
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "r2-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("real SDK client connect over streamable-HTTP: %v", err)
	}
	defer func() { _ = session.Close() }()

	// 1. initialize — the mount injects capabilities.tasks into the handshake
	//    response. The go-sdk client has no native capabilities.tasks field, so
	//    it silently drops the key on parse (the experimental extension is
	//    outside the SDK's typed ServerCapabilities — RFC §8.2); a real Tasks
	//    client reads the raw initialize result. The capability is therefore
	//    verified on the wire: a raw initialize POST to the SAME real server,
	//    exercising the mount's SSE-aware injection path on the real transport.
	initCaps := postInitializeCapabilities(t, httpSrv.URL)
	rawTasks, ok := initCaps["tasks"]
	if !ok {
		t.Fatalf("initialize handshake over the real server carries no capabilities.tasks block — got %v", initCaps)
	}
	if _, ok := initCaps["tools"]; !ok {
		t.Fatal("the SDK's own capabilities were dropped during tasks-capability injection")
	}
	tasksCap, present, err := codec.DecodeTasksServerCapability(rawTasks)
	if err != nil {
		t.Fatalf("decode capabilities.tasks: %v (raw %s)", err, rawTasks)
	}
	if !present {
		t.Fatalf("capabilities.tasks decoded as not present (raw %s)", rawTasks)
	}
	if !tasksCap.ToolsCall || !tasksCap.Cancel || !tasksCap.List {
		t.Fatalf("advertised tasks capability = %+v, want toolsCall+cancel+list on", tasksCap)
	}
	// The real SDK client completed initialize against the same server — its
	// session is live, proving the mount did not break the SDK handshake.
	if session.InitializeResult() == nil {
		t.Fatal("the real SDK client's initialize handshake did not complete")
	}

	// 2. a task-augmented tools/call over the real SDK client: the tool handler
	//    creates a Task and returns its identity.
	callRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "generate_report",
		Arguments: r2ReportInput{Account: "acme"},
	})
	if err != nil {
		t.Fatalf("task-augmented tools/call over the real transport: %v", err)
	}
	if callRes.IsError {
		t.Fatalf("tools/call returned IsError: %+v", callRes.Content)
	}
	structuredRaw, err := json.Marshal(callRes.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var created r2ReportOutput
	if err := json.Unmarshal(structuredRaw, &created); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if created.TaskID == "" {
		t.Fatal("task-augmented tools/call returned no task id")
	}
	if created.TaskStatus != string(protocolcodec.TaskWorking) {
		t.Fatalf("created task status = %q, want working", created.TaskStatus)
	}
	id := created.TaskID

	// 3. tasks/get over the real server — non-blocking, working while the task
	//    runs. tasks/* are spoken as raw JSON-RPC: a real Tasks client speaks
	//    the experimental extension's methods directly, the SDK cannot.
	getResp := r2RPC(ctx, t, httpSrv.URL, tasks.MethodGet, taskIDParams(t, id))
	if errRaw, isErr := getResp["error"]; isErr {
		t.Fatalf("tasks/get over the real server errored: %s", errRaw)
	}
	polled, err := codec.DecodeGetTaskResult(getResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/get result: %v", err)
	}
	if polled.ID != id {
		t.Fatalf("tasks/get returned task %q, want %q", polled.ID, id)
	}
	if polled.Status != protocolcodec.TaskWorking {
		t.Fatalf("tasks/get status = %q, want working", polled.Status)
	}

	// 4. tasks/list over the real server — the task is listed.
	listResp := r2RPC(ctx, t, httpSrv.URL, tasks.MethodList, nil)
	if errRaw, isErr := listResp["error"]; isErr {
		t.Fatalf("tasks/list over the real server errored: %s", errRaw)
	}
	list, err := codec.DecodeListTasksResult(listResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/list result: %v", err)
	}
	if len(list.Tasks) != 1 || list.Tasks[0].ID != id {
		t.Fatalf("tasks/list = %+v, want exactly the one created task", list.Tasks)
	}

	// Release the task so it can complete, then drive tasks/result.
	close(release)

	// 5. tasks/result over the real server — blocks to terminal, returns the
	//    underlying CallToolResult payload.
	resultResp := r2RPC(ctx, t, httpSrv.URL, tasks.MethodResult, taskIDParams(t, id))
	if errRaw, isErr := resultResp["error"]; isErr {
		t.Fatalf("tasks/result over the real server errored: %s", errRaw)
	}
	if len(resultResp["result"]) == 0 {
		t.Fatal("tasks/result over the real server returned an empty result")
	}

	// 6. FAILURE MODE — tasks/cancel of the now-terminal task is rejected with
	//    the spec's -32602, surfaced as a JSON-RPC error frame over the wire.
	cancelResp := r2RPC(ctx, t, httpSrv.URL, tasks.MethodCancel, taskIDParams(t, id))
	cancelErr, isErr := cancelResp["error"]
	if !isErr {
		t.Fatal("tasks/cancel of a terminal task over the real server should be a JSON-RPC error")
	}
	var jsonErr struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(cancelErr, &jsonErr); err != nil {
		t.Fatalf("decode JSON-RPC error: %v", err)
	}
	if jsonErr.Code != tasks.CodeInvalidParams {
		t.Fatalf("tasks/cancel error code = %d, want %d (-32602)",
			jsonErr.Code, tasks.CodeInvalidParams)
	}
}

// r2RPC sends one raw JSON-RPC request frame to a Dockyard server endpoint and
// decodes the response envelope. tasks/* are MCP Tasks extension methods the
// go-sdk client does not speak natively (RFC §8.2); a real Tasks client issues
// them as raw JSON-RPC over the same streamable-HTTP endpoint — which is
// exactly what the Tasks transport mount intercepts ahead of the SDK server.
func r2RPC(ctx context.Context, t *testing.T, url, method string,
	params json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		frame["params"] = params
	}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal %s frame: %v", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// A streamable-HTTP POST advertises it accepts both framings; the mount
	// answers tasks/* with plain application/json.
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s POST: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("%s decode (status %d, body %q): %v", method, resp.StatusCode, out, err)
	}
	return decoded
}
