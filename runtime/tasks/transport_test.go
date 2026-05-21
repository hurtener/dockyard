package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// rpcFrame builds a JSON-RPC v2 request frame for a tasks/* method.
func rpcFrame(t *testing.T, id int, method string, params json.RawMessage) []byte {
	t.Helper()
	frame := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		frame["params"] = params
	}
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return b
}

// TestMount_HandleFrame_RoutesTasksMethod proves the mount intercepts a
// tasks/get frame and answers it from the engine.
func TestMount_HandleFrame_RoutesTasksMethod(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	m := NewMount(e)
	ctx := context.Background()
	id := taskIDOf(t, e, instantRun([]byte(`{"isError":false}`), nil))

	frame := rpcFrame(t, 1, MethodGet, mustTaskIDParams(t, id))
	resp, handled, err := m.HandleFrame(ctx, "", frame)
	if err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}
	if !handled {
		t.Fatal("HandleFrame did not handle a tasks/get frame")
	}
	var got jsonRPCResponse
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error != nil {
		t.Fatalf("tasks/get frame errored: %+v", got.Error)
	}
	if string(got.ID) != "1" {
		t.Fatalf("response id = %s, want 1", got.ID)
	}
}

// TestMount_HandleFrame_PassesThroughNonTasks proves a non-tasks frame is not
// handled — the caller forwards it to the SDK server.
func TestMount_HandleFrame_PassesThroughNonTasks(t *testing.T) {
	t.Parallel()
	m := NewMount(newEngine(t, nil))
	frame := rpcFrame(t, 2, "tools/call", json.RawMessage(`{}`))
	resp, handled, err := m.HandleFrame(context.Background(), "", frame)
	if err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}
	if handled || resp != nil {
		t.Fatal("a non-tasks frame must not be handled by the mount")
	}
}

// TestMount_HandleFrame_ErrorBecomesJSONRPCError proves a Tasks engine error is
// surfaced as a JSON-RPC error object, never a panic.
func TestMount_HandleFrame_ErrorBecomesJSONRPCError(t *testing.T) {
	t.Parallel()
	m := NewMount(newEngine(t, nil))
	frame := rpcFrame(t, 3, MethodGet, mustTaskIDParams(t, "task_unknown"))
	resp, handled, err := m.HandleFrame(context.Background(), "", frame)
	if err != nil || !handled {
		t.Fatalf("HandleFrame: err=%v handled=%v", err, handled)
	}
	var got jsonRPCResponse
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error == nil {
		t.Fatal("an unknown-task tasks/get must yield a JSON-RPC error")
	}
	if got.Error.Code != CodeInvalidParams {
		t.Fatalf("error code = %d, want %d", got.Error.Code, CodeInvalidParams)
	}
}

// TestMount_HTTPMiddleware_ServesTasksFrame drives a tasks/* request over a
// real HTTP server: the middleware answers it and the inner SDK-stand-in
// handler is never reached.
func TestMount_HTTPMiddleware_ServesTasksFrame(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	m := NewMount(e)
	id := taskIDOf(t, e, instantRun([]byte(`{"isError":false}`), nil))

	innerCalled := false
	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { innerCalled = true })
	srv := httptest.NewServer(m.HTTPMiddleware(inner))
	defer srv.Close()

	frame := rpcFrame(t, 7, MethodGet, mustTaskIDParams(t, id))
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var rpc jsonRPCResponse
	if err := json.Unmarshal(body, &rpc); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if rpc.Error != nil {
		t.Fatalf("tasks/get over HTTP errored: %+v", rpc.Error)
	}
	if innerCalled {
		t.Fatal("the inner SDK handler was reached for a tasks/* frame")
	}
}

// TestMount_HTTPMiddleware_ForwardsNonTasks proves a non-tasks POST is
// forwarded to the inner handler with its body intact.
func TestMount_HTTPMiddleware_ForwardsNonTasks(t *testing.T) {
	t.Parallel()
	m := NewMount(newEngine(t, nil))

	var seenBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seenBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(m.HTTPMiddleware(inner))
	defer srv.Close()

	frame := rpcFrame(t, 8, "tools/call", json.RawMessage(`{"name":"x"}`))
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(seenBody, "tools/call") {
		t.Fatalf("inner handler did not see the forwarded body: %q", seenBody)
	}
}

// TestMount_HTTPMiddleware_AuthContextBinding proves the HTTP middleware
// applies the AuthContextFunc so a cross-context tasks/get over HTTP is
// rejected.
func TestMount_HTTPMiddleware_AuthContextBinding(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	id := mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))

	// The middleware reads the auth context from a header.
	m := NewMount(e).WithAuthContext(func(r *http.Request) string {
		return r.Header.Get("X-Auth")
	})
	srv := httptest.NewServer(m.HTTPMiddleware(http.NotFoundHandler()))
	defer srv.Close()

	post := func(authCtx string) *jsonRPCResponse {
		frame := rpcFrame(t, 9, MethodGet, mustTaskIDParams(t, id))
		req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(frame))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth", authCtx)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		var rpc jsonRPCResponse
		if err := json.Unmarshal(body, &rpc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return &rpc
	}
	if alice := post("alice"); alice.Error != nil {
		t.Fatalf("alice's own tasks/get over HTTP rejected: %+v", alice.Error)
	}
	if bob := post("bob"); bob.Error == nil {
		t.Fatal("bob's cross-context tasks/get over HTTP was not rejected")
	}
}

// TestMount_CapabilityFrame proves the mount surfaces the engine's tasks
// capability block for handshake injection.
func TestMount_CapabilityFrame(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	m := NewMount(e)
	raw, err := m.CapabilityFrame()
	if err != nil {
		t.Fatalf("CapabilityFrame: %v", err)
	}
	capBlock, ok, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).
		DecodeTasksServerCapability(raw)
	if err != nil || !ok {
		t.Fatalf("decode capability: ok=%v err=%v", ok, err)
	}
	if !capBlock.List || !capBlock.Cancel || !capBlock.ToolsCall {
		t.Fatalf("capability frame = %+v, want all gates on", capBlock)
	}
}

// TestMount_HTTPMiddleware_InjectsTasksCapability proves the middleware merges
// the capabilities.tasks block into the SDK's initialize response.
func TestMount_HTTPMiddleware_InjectsTasksCapability(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	m := NewMount(e)

	// A stand-in SDK handler that returns an initialize result with a
	// capabilities object — the shape the real SDK produces.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"serverInfo":{"name":"x"}}}`))
	})
	srv := httptest.NewServer(m.HTTPMiddleware(inner))
	defer srv.Close()

	frame := rpcFrame(t, 1, "initialize", json.RawMessage(`{"protocolVersion":"2025-06-18"}`))
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	var envelope struct {
		Result struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if _, ok := envelope.Result.Capabilities["tasks"]; !ok {
		t.Fatalf("capabilities.tasks not injected into initialize response: %s", body)
	}
	if _, ok := envelope.Result.Capabilities["tools"]; !ok {
		t.Fatal("the SDK's own capabilities were dropped during injection")
	}
}

// TestMount_HTTPMiddleware_InjectsTasksCapability_SSE proves the middleware
// merges the capabilities.tasks block into an SSE-framed (text/event-stream)
// initialize response — the framing the real go-sdk streamable-HTTP transport
// uses. The plain-JSON case alone left a real wiring gap: the capability was
// silently dropped on the wire of a real HTTP deployment (D-072).
func TestMount_HTTPMiddleware_InjectsTasksCapability_SSE(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	m := NewMount(e)

	// A stand-in SDK handler that frames the initialize result as SSE — exactly
	// the go-sdk streamable-HTTP shape: `event: message` then a `data:` line.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message\n" +
				`data: {"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"serverInfo":{"name":"x"}}}` +
				"\n\n"))
	})
	srv := httptest.NewServer(m.HTTPMiddleware(inner))
	defer srv.Close()

	frame := rpcFrame(t, 1, "initialize", json.RawMessage(`{"protocolVersion":"2025-06-18"}`))
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	// Extract the JSON-RPC envelope from the SSE data line.
	var dataLine []byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		if d := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))); len(d) > 0 && d[0] == '{' {
			dataLine = d
			break
		}
	}
	if dataLine == nil {
		t.Fatalf("SSE initialize response carries no data line: %s", body)
	}
	var envelope struct {
		Result struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(dataLine, &envelope); err != nil {
		t.Fatalf("decode SSE initialize envelope: %v", err)
	}
	if _, ok := envelope.Result.Capabilities["tasks"]; !ok {
		t.Fatalf("capabilities.tasks not injected into the SSE initialize response: %s", body)
	}
	if _, ok := envelope.Result.Capabilities["tools"]; !ok {
		t.Fatal("the SDK's own capabilities were dropped during SSE injection")
	}
}

// TestMount_ServeStdioFrames proves the stdio frame pump intercepts a tasks/*
// frame and forwards a non-tasks frame.
func TestMount_ServeStdioFrames(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	m := NewMount(e)
	id := taskIDOf(t, e, instantRun([]byte(`{"isError":false}`), nil))

	in := bytes.NewBuffer(nil)
	in.Write(rpcFrame(t, 1, MethodGet, mustTaskIDParams(t, id)))
	in.WriteByte('\n')
	in.Write(rpcFrame(t, 2, "tools/call", json.RawMessage(`{}`)))
	in.WriteByte('\n')

	var out bytes.Buffer
	forwarded := 0
	forward := func(_ context.Context, _ []byte) ([]byte, error) {
		forwarded++
		return []byte(`{"jsonrpc":"2.0","id":2,"result":{}}`), nil
	}
	if err := m.ServeStdioFrames(context.Background(), in, &out, forward); err != nil {
		t.Fatalf("ServeStdioFrames: %v", err)
	}
	if forwarded != 1 {
		t.Fatalf("forwarded %d non-tasks frames, want 1", forwarded)
	}
	// Two response lines: one from the mount (tasks/get), one forwarded.
	lines := strings.Count(strings.TrimSpace(out.String()), "\n") + 1
	if lines != 2 {
		t.Fatalf("stdio pump wrote %d response lines, want 2:\n%s", lines, out.String())
	}
}
