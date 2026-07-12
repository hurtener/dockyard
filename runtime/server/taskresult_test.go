package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

type taskToolInput struct{}
type taskToolOutput struct {
	Message string `json:"message"`
}

func TestModernToolsCallReturnsFlatCreateTaskResultOverSDKHTTP(t *testing.T) {
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		GenerateID: func() (string, error) { return "created-over-sdk", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-result", Version: "1"}, &server.Options{
		Tasks:            engine,
		TasksAuthContext: func(*http.Request) string { return "principal-a" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start", AuthContext: tasks.RequestAuthContext(ctx),
				Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"content":[]}`), nil },
			}, true)
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	var response struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	if string(response.Result["resultType"]) != `"task"` || string(response.Result["taskId"]) != `"created-over-sdk"` {
		t.Fatalf("tools/call result = %s, want flat CreateTaskResult", raw)
	}
	if _, nested := response.Result["task"]; nested {
		t.Fatalf("modern CreateTaskResult unexpectedly uses legacy task wrapper: %s", raw)
	}
	if _, err := engine.DispatchModern(context.Background(), "principal-b", tasks.MethodGet,
		tasks.ModernRequest{TaskID: "created-over-sdk"}); err == nil {
		t.Fatal("created task was not bound to request auth context")
	}
}

func TestModernRequiredTaskRejectsMissingClientCapability(t *testing.T) {
	engine, _ := tasks.NewEngine(tasks.NewInMemoryStore(), nil)
	s, _ := server.New(server.Info{Name: "task-required", Version: "1"}, &server.Options{Tasks: engine})
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }}, true)
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, _ := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	ts := httptest.NewServer(h)
	defer ts.Close()
	raw := modernToolCall(t, ts, `{}`)
	if !strings.Contains(string(raw), `"code":-32021`) || !strings.Contains(string(raw), `"io.modelcontextprotocol/tasks"`) {
		t.Fatalf("response = %s, want missing-required-client-capability error", raw)
	}
	var response struct {
		Error struct {
			Data struct {
				RequiredCapabilities map[string]json.RawMessage `json:"requiredCapabilities"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	caps := response.Error.Data.RequiredCapabilities
	if len(caps) != 1 || caps["extensions"] == nil || caps["roots"] != nil {
		t.Fatalf("required capabilities = %s, want extensions only", caps)
	}
}

func modernToolCall(t *testing.T, ts *httptest.Server, capabilities string) []byte {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"start","arguments":{},"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},` +
		`"io.modelcontextprotocol/clientCapabilities":` + capabilities + `}}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "start")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if strings.HasPrefix(string(raw), "event: message\n") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data: ") {
				return []byte(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	return raw
}
