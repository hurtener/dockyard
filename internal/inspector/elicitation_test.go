package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestElicitationFromServer_DeliversTasksResult drives the
// ElicitationFromServer adapter against a stand-in tasks server that
// accepts a single tasks/result frame and answers with an empty result.
// Proves the wire shape is correct: the inspector's HTTP request type
// maps onto a raw JSON-RPC `tasks/result` frame with `taskId` +
// `elicitedInput` params.
func TestElicitationFromServer_DeliversTasksResult(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer stand.Close()

	elicitor := ElicitationFromServer(stand.URL)
	resp, err := elicitor(context.Background(), ElicitationRequest{
		Protocol: legacyProtocol,
		TaskID:   "task-123",
		InputResponses: map[string]json.RawMessage{
			"approval": json.RawMessage(`{"approved":true,"reason":"OK"}`),
		},
	})
	if err != nil {
		t.Fatalf("elicitor: %v", err)
	}
	if resp == nil || !resp.Delivered {
		t.Fatalf("want delivered=true, got %+v", resp)
	}
	if resp.TaskID != "task-123" {
		t.Errorf("TaskID = %q, want \"task-123\"", resp.TaskID)
	}

	// Verify the wire shape — the Dockyard-internal supplyInput method.
	if got := captured["method"]; got != "dockyard/tasks/supplyInput" {
		t.Errorf("method = %v, want dockyard/tasks/supplyInput", got)
	}
	params, ok := captured["params"].(map[string]any)
	if !ok {
		t.Fatalf("params not a map: %v", captured["params"])
	}
	if params["taskId"] != "task-123" {
		t.Errorf("params.taskId = %v, want task-123", params["taskId"])
	}
	data, ok := params["data"].(map[string]any)
	if !ok {
		t.Fatalf("data not a map: %v", params["data"])
	}
	if data["approved"] != true {
		t.Errorf("approved = %v, want true", data["approved"])
	}
	if data["reason"] != "OK" {
		t.Errorf("reason = %v, want OK", data["reason"])
	}
}

// TestElicitationFromServer_DeclinedShape covers the declined-only path:
// no `data`, declined=true reaches `elicitedInput.declined`.
func TestElicitationFromServer_DeclinedShape(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer stand.Close()

	elicitor := ElicitationFromServer(stand.URL)
	resp, err := elicitor(context.Background(), ElicitationRequest{
		Protocol: legacyProtocol,
		TaskID:   "task-decl",
		InputResponses: map[string]json.RawMessage{
			"approval": json.RawMessage(`{"action":"decline"}`),
		},
	})
	if err != nil {
		t.Fatalf("elicitor: %v", err)
	}
	if !resp.Delivered {
		t.Errorf("Delivered = false")
	}
	params := captured["params"].(map[string]any)
	data := params["data"].(map[string]any)
	if data["action"] != "decline" {
		t.Errorf("params.data.action = %v, want decline", data["action"])
	}
}

// TestElicitationFromServer_ServerErrorIsDeliveredFalse — when the
// server answers a JSON-RPC error block, the call is a successful RPC
// (delivered to the server, server refused) — Delivered=false +
// Error set, no transport-level error.
func TestElicitationFromServer_ServerErrorIsDeliveredFalse(t *testing.T) {
	t.Parallel()
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"task already terminal"}}`))
	}))
	defer stand.Close()

	resp, err := ElicitationFromServer(stand.URL)(context.Background(), ElicitationRequest{
		Protocol: legacyProtocol,
		TaskID:   "stale",
		InputResponses: map[string]json.RawMessage{
			"approval": json.RawMessage(`{"approved":true}`),
		},
	})
	if err != nil {
		t.Fatalf("elicitor returned a transport error on a server error: %v", err)
	}
	if resp.Delivered {
		t.Errorf("Delivered = true, want false on a server error")
	}
	if resp.Error == "" {
		t.Errorf("Error should carry the server's message")
	}
}

// TestElicitationFromServer_DetachedReturnsError — an empty baseURL is
// the detached case; the adapter returns a typed error.
func TestElicitationFromServer_DetachedReturnsError(t *testing.T) {
	t.Parallel()
	resp, err := ElicitationFromServer("")(context.Background(), ElicitationRequest{
		Protocol: modernProtocol, TaskID: "x", InputResponses: map[string]json.RawMessage{},
	})
	if err == nil {
		t.Fatal("want a typed detached error, got nil")
	}
	if resp != nil {
		t.Errorf("want nil response on detached, got %+v", resp)
	}
}

func TestTaskRoutingTransportAddsLifecycleHeadersWithoutChangingBody(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"tasks/get", "tasks/update", "tasks/cancel"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			body := []byte(`{"jsonrpc":"2.0", "id":7, "method":"` + method + `", "params":{"taskId":"task-123","inputResponses":{"approval":true}}}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read body: %v", err)
				}
				if !bytes.Equal(gotBody, body) {
					t.Errorf("body changed:\n got: %s\nwant: %s", gotBody, body)
				}
				if got := r.Header.Get("Mcp-Method"); got != method {
					t.Errorf("Mcp-Method = %q, want %q", got, method)
				}
				if got := r.Header.Get("Mcp-Name"); got != "task-123" {
					t.Errorf("Mcp-Name = %q, want task-123", got)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := (&http.Client{Transport: taskRoutingTransport{base: http.DefaultTransport}}).Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			_ = resp.Body.Close()
		})
	}
}

// TestElicitationFromServer_NonOKStatusIsError — a 5xx surfaces as a
// typed error (truncated body in the message).
func TestElicitationFromServer_NonOKStatusIsError(t *testing.T) {
	t.Parallel()
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer stand.Close()
	_, err := ElicitationFromServer(stand.URL)(context.Background(), ElicitationRequest{
		Protocol: legacyProtocol, TaskID: "x",
		InputResponses: map[string]json.RawMessage{"answer": json.RawMessage(`{}`)},
	})
	if err == nil {
		t.Fatal("want a transport error, got nil")
	}
}

// TestAssetsMux_Elicitation_Detached covers the 503 path: no Elicitor
// is wired, the endpoint surfaces an honest "detached" message.
func TestAssetsMux_Elicitation_Detached(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{}, slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/elicitation", bytes.NewReader(
		[]byte(`{"protocol":"2026-07-28","taskId":"x","inputResponses":{}}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("expected an error message, got %v", body)
	}
}

// TestAssetsMux_Elicitation_BadBody covers the 400 path: an
// undecodable body is rejected.
func TestAssetsMux_Elicitation_BadBody(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{
		Elicitor: func(_ context.Context, _ ElicitationRequest) (*ElicitationResponse, error) {
			return &ElicitationResponse{Delivered: true}, nil
		},
	}, slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/elicitation",
		bytes.NewReader([]byte(`{"unknown":1}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestAssetsMux_Elicitation_RequiresTaskID — a body missing taskId is
// 400.
func TestAssetsMux_Elicitation_RequiresTaskID(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{
		Elicitor: func(_ context.Context, _ ElicitationRequest) (*ElicitationResponse, error) {
			t.Fatal("elicitor should not be called when taskId is missing")
			return nil, nil
		},
	}, slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/elicitation",
		bytes.NewReader([]byte(`{"protocol":"2026-07-28","taskId":"","inputResponses":{}}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestAssetsMux_Elicitation_Success drives the happy 200 path.
func TestAssetsMux_Elicitation_Success(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{
		Elicitor: func(_ context.Context, req ElicitationRequest) (*ElicitationResponse, error) {
			if req.TaskID != "task-99" {
				t.Errorf("TaskID = %q, want task-99", req.TaskID)
			}
			if req.Protocol != modernProtocol {
				t.Errorf("Protocol = %q, want %q", req.Protocol, modernProtocol)
			}
			if got := string(req.InputResponses["approval"]); got != `{"action":"accept"}` {
				t.Errorf("inputResponses.approval = %s", got)
			}
			return &ElicitationResponse{TaskID: req.TaskID, Delivered: true}, nil
		},
	}, slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/elicitation",
		bytes.NewReader([]byte(`{"protocol":"2026-07-28","taskId":"task-99","inputResponses":{"approval":{"action":"accept"}}}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp ElicitationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Delivered {
		t.Errorf("Delivered = false, want true")
	}
}

// TestAssetsMux_Elicitation_ErrorIs502 — an Elicitor error is mapped to
// 502 with a typed JSON body.
func TestAssetsMux_Elicitation_ErrorIs502(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{
		Elicitor: func(_ context.Context, _ ElicitationRequest) (*ElicitationResponse, error) {
			return nil, errors.New("upstream offline")
		},
	}, slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/elicitation",
		bytes.NewReader([]byte(`{"protocol":"2026-07-28","taskId":"x","inputResponses":{"answer":{"action":"accept"}}}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}
