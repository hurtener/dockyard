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

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

const (
	elicitationTimeout = 30 * time.Second
	modernProtocol     = "2026-07-28"
	legacyProtocol     = "2025-11-25"
	maxLegacyResponse  = 1 << 20
)

var modernTaskMethods = map[string]struct{}{
	"tasks/get":    {},
	"tasks/update": {},
	"tasks/cancel": {},
}

// ElicitationRequest is the typed request POSTed to /api/tasks/elicitation.
// InputResponses retains the server-assigned request keys required by modern
// tasks/update. Protocol makes use of the legacy Dockyard extension explicit.
type ElicitationRequest struct {
	Protocol       string                     `json:"protocol"`
	TaskID         string                     `json:"taskId"`
	InputResponses map[string]json.RawMessage `json:"inputResponses"`
}

// ElicitationResponse reports whether task input was delivered to the server.
type ElicitationResponse struct {
	TaskID    string `json:"taskId"`
	Delivered bool   `json:"delivered"`
	Error     string `json:"error,omitempty"`
}

// Elicitor delivers task input to a connected Dockyard server.
type Elicitor func(context.Context, ElicitationRequest) (*ElicitationResponse, error)

// ElicitationFromServer returns an Elicitor that targets baseURL.
func ElicitationFromServer(baseURL string) Elicitor {
	return func(ctx context.Context, req ElicitationRequest) (*ElicitationResponse, error) {
		if baseURL == "" {
			return nil, errors.New("dockyard/internal/inspector: inspector is detached — no server URL to deliver elicitations to")
		}
		switch req.Protocol {
		case modernProtocol:
			return deliverModernTaskInput(ctx, baseURL, req)
		case legacyProtocol:
			return deliverLegacyTaskInput(ctx, baseURL, req)
		default:
			return nil, fmt.Errorf("dockyard/internal/inspector: unsupported task protocol %q", req.Protocol)
		}
	}
}

type taskParams struct {
	mcpsdk.ParamsBase
	TaskID         string                     `json:"taskId"`
	InputResponses map[string]json.RawMessage `json:"inputResponses,omitempty"`
}

type taskResult struct {
	mcpsdk.ResultBase
}

func deliverModernTaskInput(ctx context.Context, baseURL string, req ElicitationRequest) (*ElicitationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, elicitationTimeout)
	defer cancel()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-inspector", Version: "0.1.0"},
		modernTaskClientOptions(),
	)
	for method := range modernTaskMethods {
		if err := mcpsdk.AddSendingCustomMethod[*taskParams, *taskResult](client, method); err != nil {
			return nil, fmt.Errorf("dockyard/internal/inspector: register %s: %w", method, err)
		}
	}
	httpClient := modernFirstHTTPClient(elicitationTimeout,
		taskRoutingTransport{base: http.DefaultTransport}, false)
	session, err := client.Connect(ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: baseURL, HTTPClient: httpClient}, nil)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: connect %q: %w", baseURL, err)
	}
	defer func() { _ = session.Close() }()

	_, err = mcpsdk.CallCustomMethod[*taskParams, *taskResult](ctx, session, "tasks/update", &taskParams{
		TaskID: req.TaskID, InputResponses: req.InputResponses,
	})
	if err != nil {
		return &ElicitationResponse{TaskID: req.TaskID, Error: err.Error()}, nil
	}
	return &ElicitationResponse{TaskID: req.TaskID, Delivered: true}, nil
}

func modernTaskClientOptions() *mcpsdk.ClientOptions {
	return &mcpsdk.ClientOptions{
		Capabilities: &mcpsdk.ClientCapabilities{Extensions: map[string]any{
			protocolcodec.ModernTasksExtension: map[string]any{},
		}},
		MultiRoundTrip: &mcpsdk.MultiRoundTripOptions{Disabled: true},
	}
}

type taskRoutingTransport struct {
	base http.RoundTripper
}

func (t taskRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: read custom method request: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))

	var routing struct {
		Method string `json:"method"`
		Params struct {
			TaskID string `json:"taskId"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &routing) == nil {
		if _, ok := modernTaskMethods[routing.Method]; ok && routing.Params.TaskID != "" {
			req.Header.Set("Mcp-Method", routing.Method)
			req.Header.Set("Mcp-Name", routing.Params.TaskID)
		}
	}
	return t.base.RoundTrip(req)
}

func deliverLegacyTaskInput(ctx context.Context, baseURL string, req ElicitationRequest) (*ElicitationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, elicitationTimeout)
	defer cancel()

	if len(req.InputResponses) != 1 {
		return nil, errors.New("dockyard/internal/inspector: legacy task input requires exactly one keyed input response")
	}
	var data json.RawMessage
	for _, response := range req.InputResponses {
		data = response
	}
	params := map[string]any{"taskId": req.TaskID, "data": data}
	frame := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "dockyard/tasks/supplyInput", "params": params,
	}
	body, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: marshal legacy task input frame: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: new legacy task input request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := (&http.Client{Timeout: elicitationTimeout}).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: legacy task input POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxLegacyResponse+1))
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: read legacy task input response: %w", err)
	}
	if len(out) > maxLegacyResponse {
		return nil, errors.New("dockyard/internal/inspector: legacy task input response exceeds 1 MiB")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("dockyard/internal/inspector: legacy task input status %d: %s", resp.StatusCode, truncate(out, 256))
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: decode legacy task input envelope: %w (body %s)", err, truncate(out, 256))
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("dockyard/internal/inspector: legacy task input response contains trailing JSON")
	}
	if envelope.JSONRPC != "2.0" || !bytes.Equal(bytes.TrimSpace(envelope.ID), []byte("1")) {
		return nil, errors.New("dockyard/internal/inspector: malformed legacy task input JSON-RPC envelope")
	}
	hasResult := len(envelope.Result) > 0 && string(bytes.TrimSpace(envelope.Result)) != "null"
	hasError := len(envelope.Error) > 0 && string(bytes.TrimSpace(envelope.Error)) != "null"
	if hasResult == hasError {
		return nil, errors.New("dockyard/internal/inspector: legacy task input response must contain exactly one of result or error")
	}
	if hasError {
		var rpcErr struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(envelope.Error, &rpcErr); err != nil || rpcErr.Code == nil || rpcErr.Message == "" {
			return nil, errors.New("dockyard/internal/inspector: malformed legacy task input JSON-RPC error")
		}
		return &ElicitationResponse{TaskID: req.TaskID, Error: rpcErr.Message}, nil
	}
	return &ElicitationResponse{TaskID: req.TaskID, Delivered: true}, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
