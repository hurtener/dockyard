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

// TestModernTaskResultsCarryServerInfoAndResultType proves the v1.9.1 SEP-2575
// serverInfo injection (D-199) reaches modern-protocol Tasks results. Over
// stateless HTTP 2026-07-28, tasks/get, tasks/update, and tasks/cancel are
// served by the SDK custom methods and therefore flow through
// responseSemanticsMiddleware — so each successful result carries both the
// resultType discriminator and the serverInfo _meta a non-Tasks modern result
// carries. This is the reachable modern Tasks path; the Tasks transport mount
// (legacy codec) never serves a modern session (see TestModernProtocol
// UnreachableViaInitialize).
func TestModernTaskResultsCarryServerInfoAndResultType(t *testing.T) {
	work := make(chan struct{})
	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{
		GenerateID:    func() (string, error) { return "modern-task", nil },
		AdvertiseList: true, RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	s, err := server.New(server.Info{Name: "parity-server", Version: "9.9.9"}, &server.Options{Tasks: engine})
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
	t.Cleanup(func() { close(work) })

	for _, tc := range []struct {
		method, params string
	}{
		{"tasks/get", `{"taskId":"modern-task"}`},
		{"tasks/update", `{"taskId":"modern-task","inputResponses":{}}`},
		{"tasks/cancel", `{"taskId":"modern-task"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			raw := modernPost(t, ts, tc.method, "modern-task", tc.params)
			if !strings.Contains(raw, `"resultType":"complete"`) {
				t.Errorf("%s result missing resultType: %s", tc.method, raw)
			}
			name, version := serverInfoFromResult(t, raw)
			if name != "parity-server" || version != "9.9.9" {
				t.Errorf("%s serverInfo = {name:%q version:%q}, want {parity-server 9.9.9}: %s",
					tc.method, name, version, raw)
			}
		})
	}
}

// TestModernProtocolUnreachableViaInitialize proves the modern 2026-07-28
// protocol cannot be negotiated through the initialize handshake: the SDK caps
// initialize negotiation below 2026-07-28. Because stdio speaks only the
// initialize lifecycle (the modern protocol replaces initialize with the
// stateless server/discover flow that only the HTTP stateless handler serves),
// a stdio session is always legacy — which is why the Tasks transport mount's
// legacy codec is correct over stdio and never emits a modern-shaped Tasks
// response there.
func TestModernProtocolUnreachableViaInitialize(t *testing.T) {
	s, err := server.New(server.Info{Name: "init-cap", Version: "1"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	negotiated := negotiatedProtocolVersion(t, raw)
	if negotiated == "2026-07-28" {
		t.Fatalf("initialize negotiated the modern protocol, which must be unreachable off the stateless HTTP path: %s", raw)
	}
	if negotiated != "2025-11-25" {
		t.Fatalf("initialize negotiated %q, want the legacy cap 2025-11-25: %s", negotiated, raw)
	}
}

// serverInfoFromResult extracts result._meta["io.modelcontextprotocol/serverInfo"]
// name/version from a JSON-RPC response frame (SSE or JSON).
func serverInfoFromResult(t *testing.T, frame string) (name, version string) {
	t.Helper()
	var env struct {
		Result struct {
			Meta struct {
				ServerInfo struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"io.modelcontextprotocol/serverInfo"`
			} `json:"_meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(extractJSONRPCPayload(frame)), &env); err != nil {
		t.Fatalf("decode result frame %q: %v", frame, err)
	}
	return env.Result.Meta.ServerInfo.Name, env.Result.Meta.ServerInfo.Version
}

func negotiatedProtocolVersion(t *testing.T, frame []byte) string {
	t.Helper()
	var env struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(extractJSONRPCPayload(string(frame))), &env); err != nil {
		t.Fatalf("decode initialize frame %q: %v", frame, err)
	}
	return env.Result.ProtocolVersion
}

// extractJSONRPCPayload returns the JSON object from a response body that may be
// either a raw JSON-RPC frame or a text/event-stream "data:" line.
func extractJSONRPCPayload(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(body)
}
