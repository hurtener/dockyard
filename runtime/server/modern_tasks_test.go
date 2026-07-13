package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

func TestStatelessDiscoveryAndModernTaskMethods(t *testing.T) {
	work := make(chan struct{})
	appCapability, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("Apps capability: %v", err)
	}
	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{
		GenerateID:    func() (string, error) { return "modern-task", nil },
		AdvertiseList: true, RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	s, err := server.New(server.Info{Name: "modern", Version: "1"}, &server.Options{
		Extensions: []server.ExtensionCapability{appCapability}, Tasks: engine,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := engine.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "work", Run: func(context.Context) (json.RawMessage, error) {
			<-work
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	discover := modernPost(t, ts, "server/discover", "", `{}`)
	if !strings.Contains(discover, apps.ExtensionID) || !strings.Contains(discover, "io.modelcontextprotocol/tasks") {
		t.Fatalf("server/discover did not advertise Apps and Tasks: %s", discover)
	}
	got := modernPost(t, ts, "tasks/get", "modern-task", `{"taskId":"modern-task"}`)
	if !strings.Contains(got, "modern-task") {
		t.Fatalf("tasks/get response = %s", got)
	}
	if err := store.AddInputRequest(context.Background(), "modern-task", tasks.InputRequest{
		Key: "roots", Method: tasks.InputMethodRoots,
		Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
	}); err != nil {
		t.Fatalf("AddInputRequest: %v", err)
	}
	response := modernPost(t, ts, "tasks/update", "modern-task", `{"taskId":"modern-task","inputResponses":{}}`)
	if !strings.Contains(response, `"resultType":"complete"`) {
		t.Fatalf("tasks/update response = %s, want complete acknowledgement", response)
	}
	response = modernPost(t, ts, "tasks/cancel", "modern-task", `{"taskId":"modern-task"}`)
	if !strings.Contains(response, `"resultType":"complete"`) {
		t.Fatalf("tasks/cancel response = %s, want complete acknowledgement", response)
	}
	close(work)
	listed := modernPost(t, ts, "tasks/list", "", `{}`)
	if !strings.Contains(listed, `"code":-32601`) {
		t.Fatalf("modern tasks/list response = %s, want method not found", listed)
	}
}

func TestModernTaskCustomMethodErrorCodes(t *testing.T) {
	engine, _ := tasks.NewEngine(tasks.NewInMemoryStore(), nil)
	s, _ := server.New(server.Info{Name: "modern-errors", Version: "1"}, &server.Options{
		Tasks: engine, TasksAuthContext: func(*http.Request) string { return "mallory" },
	})
	h, _ := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	for _, tc := range []struct {
		name, method, taskID, params string
		code                         int
	}{
		{"unknown task", "tasks/get", "missing", `{"taskId":"missing"}`, -32602},
		{"malformed get", "tasks/get", "", `{}`, -32602},
		{"malformed update", "tasks/update", "x", `{"taskId":"x"}`, -32602},
		{"unsupported", "tasks/result", "x", `{"taskId":"x"}`, -32601},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := modernPost(t, ts, tc.method, tc.taskID, tc.params)
			if !strings.Contains(got, `"code":`+fmt.Sprint(tc.code)) {
				t.Fatalf("response = %s, want code %d", got, tc.code)
			}
		})
	}
}

func modernPost(t *testing.T, ts *httptest.Server, method, name, params string) string {
	t.Helper()
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	params = strings.TrimSuffix(params, "}")
	if params != "{" {
		params += ","
	}
	params += meta + "}"
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
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
	return string(raw)
}
