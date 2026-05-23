package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// elicitationTimeout bounds one operator-initiated tasks/result delivery —
// a POST against the attached server's transport. The inspector is a dev
// surface; a server that does not answer promptly surfaces as a typed
// error rather than stalling the UI.
const elicitationTimeout = 30 * time.Second

// ElicitationRequest is the typed request the inspector frontend POSTs to
// `/api/tasks/elicitation` (Phase 25 / D-134). The body carries the
// task id the elicitation answers and the App-supplied reply payload.
//
// Distinct from the bridge's ElicitationResponseParams (which is the
// View→host postMessage shape): this is the inspector's HTTP shape. The
// inspector backend translates one into the other.
type ElicitationRequest struct {
	// TaskID is the id of the task the elicitation answers. Required.
	TaskID string `json:"taskId"`
	// Data is the user's reply, opaque to the inspector — the receiving
	// handler decodes it against its own contract. May be absent when
	// Declined is true.
	Data json.RawMessage `json:"data,omitempty"`
	// Declined is the explicit "user declined to answer" signal. The
	// MCP Tasks experimental spec carries this through as the
	// elicited-input's declined flag.
	Declined bool `json:"declined,omitempty"`
}

// ElicitationResponse is the inspector's reply to a successful
// elicitation delivery. The reply is a bare acknowledgement — the App
// observes the task's terminal status through the subsequent
// `tool-result` push or through the inspector's Tasks panel, not
// through this response.
type ElicitationResponse struct {
	// TaskID echoes the id of the task the elicitation was delivered to.
	TaskID string `json:"taskId"`
	// Delivered is true when the attached server accepted the
	// elicitation; false (with a non-empty Error) when the server
	// refused.
	Delivered bool `json:"delivered"`
	// Error is the server's typed error message when Delivered is
	// false. Absent on a successful delivery.
	Error string `json:"error,omitempty"`
}

// Elicitor delivers one operator-initiated elicitation-response to the
// attached MCP server (Phase 25 / D-134). The inspector calls it per
// `POST /api/tasks/elicitation` request. Like [ToolInvoker] it is the
// lone mutating surface the inspector exposes for this operation:
// localhost-only via the listener's `requireLoopback` gate; the
// operator is the one driving the write through the UI (the App's
// "Approve" / "Reject" button); the inspector never speaks tasks/* on
// its own.
//
// Returns a typed error when the underlying server call fails (a
// connect or RPC error). The `/api/tasks/elicitation` handler maps a
// non-nil error to HTTP 502 with a typed JSON body so the inspector
// frontend surfaces an honest error state.
type Elicitor func(ctx context.Context, req ElicitationRequest) (*ElicitationResponse, error)

// ElicitationFromServer adapts a running MCP server, named by its base
// URL, into an [Elicitor]. The implementation speaks raw JSON-RPC: the
// MCP Tasks methods (tasks/result etc.) sit outside the go-sdk's
// dispatch table (the experimental extension — RFC §8.2), so a real
// Tasks client posts them as plain JSON-RPC frames over the same
// streamable-HTTP endpoint the server already serves. This is the same
// pattern the R2 integration test uses (`r2_tasks_mount_test.go`'s
// `r2RPC` helper) — D-134 routes a single such frame through the
// inspector's HTTP boundary.
//
// A nil baseURL yields a source that returns an error — without an
// attached server there is no task to resume, and the inspector
// frontend surfaces the error.
func ElicitationFromServer(baseURL string) Elicitor {
	return func(ctx context.Context, req ElicitationRequest) (*ElicitationResponse, error) {
		if baseURL == "" {
			return nil, errors.New(
				"dockyard/internal/inspector: inspector is detached — " +
					"no server URL to deliver elicitations to")
		}
		return deliverElicitation(ctx, baseURL, req)
	}
}

// deliverElicitation posts a raw `tasks/result` JSON-RPC frame to
// baseURL with the App-supplied reply as the elicited-input payload. A
// 200 with no JSON-RPC error in the body is a success; any other
// outcome is a typed error.
func deliverElicitation(
	ctx context.Context, baseURL string, req ElicitationRequest,
) (*ElicitationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, elicitationTimeout)
	defer cancel()

	// Build the Dockyard-internal `dockyard/tasks/supplyInput` params
	// (Phase 25 / D-134). The vendored experimental Tasks spec does
	// NOT define a standard wire shape for resuming an input_required
	// task — `tasks/result` only blocks until terminal, it does not
	// deliver the elicited input. Dockyard extends the mount with a
	// namespaced internal method (the `dockyard/` prefix) that wraps
	// [tasks.Engine.SupplyInput]; the inspector posts to it here.
	params := map[string]any{"taskId": req.TaskID}
	if len(req.Data) > 0 {
		var v any
		if err := json.Unmarshal(req.Data, &v); err != nil {
			return nil, fmt.Errorf(
				"dockyard/internal/inspector: decode elicitation data: %w", err)
		}
		// Pass the data through as a JSON value; the engine reads it
		// back as raw JSON via `InputResponse.Data`.
		dataJSON, mErr := json.Marshal(v)
		if mErr != nil {
			return nil, fmt.Errorf(
				"dockyard/internal/inspector: re-marshal elicitation data: %w", mErr)
		}
		params["data"] = json.RawMessage(dataJSON)
	}
	if req.Declined {
		params["declined"] = true
	}

	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "dockyard/tasks/supplyInput",
		"params":  params,
	}
	body, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: marshal tasks/result frame: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: new tasks/result request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: tasks/result POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: read tasks/result response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: tasks/result status %d: %s",
			resp.StatusCode, truncate(out, 256))
	}
	// Parse the JSON-RPC envelope; an `error` block is a server-side
	// refusal we surface honestly.
	var envelope struct {
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: decode tasks/result envelope: %w (body %s)",
			err, truncate(out, 256))
	}
	if envelope.Error != nil {
		return &ElicitationResponse{
			TaskID:    req.TaskID,
			Delivered: false,
			Error:     envelope.Error.Message,
		}, nil
	}
	return &ElicitationResponse{TaskID: req.TaskID, Delivered: true}, nil
}

// truncate clips a byte slice for use in an error message — avoids
// dumping a multi-kB response into a log line.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
