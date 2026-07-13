package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/inspector"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"

	_ "github.com/hurtener/dockyard/templates/approval-flows"
	afcontracts "github.com/hurtener/dockyard/templates/approval-flows/pkg/contracts"
	afhandlers "github.com/hurtener/dockyard/templates/approval-flows/pkg/handlers"
)

func TestPhase33_TemplateMaterialisesBuildsAndTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	binPath := filepath.Join(t.TempDir(), "dockyard")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "./cmd/dockyard") //nolint:gosec // fixed test command
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dockyard: %v\n%s", err, out)
	}

	parent := t.TempDir()
	cmd := exec.CommandContext(context.Background(), binPath, "new", "af-itest", "--template", "approval-flows", "--dir", parent, "--dockyard-path", root) //nolint:gosec // fixed test command
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("materialise approval-flows: %v\n%s", err, out)
	}
	project := filepath.Join(parent, "af-itest")
	for _, rel := range []string{"dockyard.app.yaml", "main.go", "internal/contracts/contracts.go", "internal/handlers/handlers.go", "web/src/App.svelte", "go.mod", "README.md"} {
		if _, err := os.Stat(filepath.Join(project, rel)); err != nil {
			t.Errorf("materialised project missing %s: %v", rel, err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(project, "dockyard.app.yaml")) //nolint:gosec // test temp directory
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: request_approval", "name: propose_with_edits", "task_support: required"} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest missing %q", want)
		}
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "./..."}, {"vet", "./..."}, {"test", "./..."}} {
		goCmd := exec.CommandContext(context.Background(), "go", args...) //nolint:gosec // fixed test table
		goCmd.Dir = project
		goCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := goCmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestPhase33_ApprovalFlowModernSuccessAndInvalidUpdate(t *testing.T) {
	t.Parallel()
	_, ts := newPhase33ApprovalServer(t, nil)
	callApproval(t, ts, "Approve release")

	get := waitModernTask(t, ts, "approval-task", "", "input_required")
	inputs := resultObject(t, get, "inputRequests")
	if _, ok := inputs["approval-decision"]; !ok {
		t.Fatalf("tasks/get inputRequests = %#v, want approval-decision", inputs)
	}

	// An unmatched response is acknowledged but must not consume persisted input.
	postModern(t, ts, "tasks/update", "", map[string]any{
		"taskId": "approval-task", "inputResponses": map[string]any{"wrong-key": map[string]any{"approved": true}},
	})
	get = postModern(t, ts, "tasks/get", "", map[string]any{"taskId": "approval-task"})
	if status := resultString(t, get, "status"); status != "input_required" {
		t.Fatalf("status after invalid update = %q, want input_required", status)
	}
	if _, ok := resultObject(t, get, "inputRequests")["approval-decision"]; !ok {
		t.Fatal("invalid update consumed the outstanding approval request")
	}

	postModern(t, ts, "tasks/update", "", map[string]any{
		"taskId": "approval-task", "inputResponses": map[string]any{"approval-decision": map[string]any{
			"action": "accept", "content": map[string]any{"approved": true, "reason": "ship it"},
		}},
	})
	completed := waitModernTask(t, ts, "approval-task", "", "completed")
	result := resultObject(t, completed, "result")
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("completed result = %#v", result)
	}
	if structured["state"] != "approved" || structured["reason"] != "ship it" || structured["approved"] != true {
		t.Fatalf("completed structuredContent = %#v", structured)
	}
}

func TestPhase33_InspectorCompletesWaitingModernTask(t *testing.T) {
	t.Parallel()
	_, ts := newPhase33ApprovalServer(t, nil)
	callApproval(t, ts, "Approve through inspector")
	waitModernTask(t, ts, "approval-task", "", "input_required")

	resp, err := inspector.ElicitationFromServer(ts.URL)(context.Background(), inspector.ElicitationRequest{
		Protocol: "2026-07-28",
		TaskID:   "approval-task",
		InputResponses: map[string]json.RawMessage{
			"approval-decision": json.RawMessage(`{"action":"accept","content":{"approved":true,"reason":"inspector approved"}}`),
		},
	})
	if err != nil {
		t.Fatalf("inspector task update: %v", err)
	}
	if resp == nil || !resp.Delivered {
		t.Fatalf("inspector task update response = %#v", resp)
	}

	completed := waitModernTask(t, ts, "approval-task", "", "completed")
	structured, ok := resultObject(t, completed, "result")["structuredContent"].(map[string]any)
	if !ok || structured["state"] != "approved" || structured["reason"] != "inspector approved" {
		t.Fatalf("completed inspector result = %#v", completed)
	}
}

func TestPhase33_ApprovalFlowModernCancellation(t *testing.T) {
	t.Parallel()
	_, ts := newPhase33ApprovalServer(t, nil)
	callApproval(t, ts, "Cancel release")
	waitModernTask(t, ts, "approval-task", "", "input_required")
	ack := postModern(t, ts, "tasks/cancel", "", map[string]any{"taskId": "approval-task"})
	if resultString(t, ack, "resultType") != "complete" {
		t.Fatalf("tasks/cancel acknowledgement = %#v", ack)
	}
	waitModernTask(t, ts, "approval-task", "", "cancelled")
}

func TestPhase33_ModernTaskIdentityBindingAcrossRequests(t *testing.T) {
	t.Parallel()
	engine, ts := newPhase33ApprovalServer(t, func(r *http.Request) string { return r.Header.Get("Authorization") })
	release := make(chan struct{})
	if _, err := engine.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "identity", AuthContext: "alice", Run: func(context.Context) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`{"ok":true}`), nil
		},
	}); err != nil {
		t.Fatalf("create identity-bound task: %v", err)
	}
	t.Cleanup(func() { close(release) })
	if got := postModern(t, ts, "tasks/get", "alice", map[string]any{"taskId": "approval-task"}); got["error"] != nil {
		t.Fatalf("alice's independent tasks/get failed: %#v", got)
	}
	if got := postModern(t, ts, "tasks/get", "bob", map[string]any{"taskId": "approval-task"}); got["error"] == nil {
		t.Fatalf("bob's cross-request tasks/get succeeded: %#v", got)
	}
}

func TestPhase33_CoreMRTRRetryCompletesWithoutTaskUpdate(t *testing.T) {
	t.Parallel()
	srv, err := server.New(server.Info{Name: "mrtr-itest", Version: "1"}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Title string `json:"title"`
	}
	type output struct {
		Approved bool `json:"approved"`
	}
	if err := tool.New[input, output]("approve").ContinuationHandler(func(_ context.Context, call tool.Call[input]) (tool.Result[output], error) {
		if call.RequestState == "" {
			return tool.Result[output]{
				InputRequests: map[string]tool.InputRequest{"decision": tool.ElicitationRequest{Message: "Approve " + call.Input.Title}},
				RequestState:  "approval-state",
			}, nil
		}
		response, ok := call.InputResponses["decision"].(tool.ElicitationResponse)
		return tool.Result[output]{Structured: output{Approved: ok && response.Action == "accept"}}, nil
	}).Register(srv); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	elicitations := 0
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "itest", Version: "1"}, &mcpsdk.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
			elicitations++
			if req.Params.Message != "Approve release" {
				t.Fatalf("elicitation message = %q", req.Params.Message)
			}
			return &mcpsdk.ElicitResult{Action: "accept"}, nil
		},
	})
	session, err := client.Connect(ctx, srv.ServeInMemory(ctx), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	retry, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "approve", Arguments: input{Title: "release"}})
	if err != nil {
		t.Fatal(err)
	}
	if elicitations != 1 {
		t.Fatalf("MRTR elicitation count = %d, want 1", elicitations)
	}
	structured, ok := retry.StructuredContent.(map[string]any)
	if retry.RequestState != "" || len(retry.InputRequests) != 0 || !ok || structured["approved"] != true {
		t.Fatalf("completed MRTR retry = %#v", retry)
	}
}

func newPhase33ApprovalServer(t *testing.T, auth tasks.AuthContextFunc) (*tasks.Engine, *httptest.Server) {
	t.Helper()
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		GenerateID: func() (string, error) { return "approval-task", nil }, RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Info{Name: "approval-itest", Version: "1"}, &server.Options{Logger: quietLogger(), Tasks: engine, TasksAuthContext: auth})
	if err != nil {
		t.Fatal(err)
	}
	h := afhandlers.CreateRequestApproval{Engine: engine}
	if err := tool.New[afcontracts.RequestApprovalInput, afcontracts.RequestApprovalOutput]("request_approval").Handler(h.Handler).Register(srv); err != nil {
		t.Fatal(err)
	}
	httpHandler, err := srv.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(httpHandler)
	t.Cleanup(ts.Close)
	return engine, ts
}

func callApproval(t *testing.T, ts *httptest.Server, title string) {
	t.Helper()
	got := postModern(t, ts, "tools/call", "", map[string]any{
		"name": "request_approval", "arguments": map[string]any{"title": title, "description": "integration test"},
	})
	if got["error"] != nil {
		t.Fatalf("tools/call failed: %#v", got)
	}
}

func waitModernTask(t *testing.T, ts *httptest.Server, id, auth, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := postModern(t, ts, "tasks/get", auth, map[string]any{"taskId": id})
		if got["error"] == nil && resultString(t, got, "status") == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", id, want)
	return nil
}

func postModern(t *testing.T, ts *httptest.Server, method, auth string, params map[string]any) map[string]any {
	t.Helper()
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientInfo":      map[string]any{"name": "integration", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{
			"extensions": map[string]any{"io.modelcontextprotocol/tasks": map[string]any{}},
		},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name, _ := params["name"].(string); name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if taskID, _ := params["taskId"].(string); taskID != "" {
		req.Header.Set("Mcp-Name", taskID)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload := raw
	if strings.HasPrefix(string(raw), "event:") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data: ") {
				payload = []byte(strings.TrimPrefix(line, "data: "))
				break
			}
		}
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode %s response: %v (%s)", method, err, raw)
	}
	return got
}

func resultString(t *testing.T, response map[string]any, key string) string {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %#v", response)
	}
	value, _ := result[key].(string)
	return value
}

func resultObject(t *testing.T, response map[string]any, key string) map[string]any {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %#v", response)
	}
	value, ok := result[key].(map[string]any)
	if !ok {
		t.Fatalf("result.%s is not an object: %#v", key, result[key])
	}
	return value
}
