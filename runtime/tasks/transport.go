package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// This file is the Tasks transport mount (RFC §8.2 — "a shim, by necessity").
//
// Phase 13 found that the go-sdk routes receiving methods through a fixed
// package-level dispatch table (`serverMethodInfos`); an unknown method —
// tasks/get, tasks/result, … — is rejected by the SDK before any middleware
// runs, so a tasks/* frame never reaches Engine.Dispatch over a real transport.
// The SDK's jsonrpc message types are unexported behind internal/, so the mount
// cannot intercept at the SDK's jsonrpc.Message layer either.
//
// The mount therefore operates at the raw JSON-RPC frame layer, which Dockyard
// is free to own: a tasks/* request frame is intercepted, served by the Engine,
// and answered directly; every other frame is forwarded untouched to the SDK
// server. The JSON-RPC v2 envelope types below are plain protocol-neutral
// JSON-RPC, NOT MCP extension wire types — the MCP Tasks wire shapes stay
// inside internal/protocolcodec (P3); the mount only reads the `method` and
// `id` of an envelope and passes `params` through verbatim.

// jsonRPCRequest is a minimal JSON-RPC v2 request envelope — enough to route on
// `method` and echo `id`. It is plain JSON-RPC, not an MCP wire type.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is a minimal JSON-RPC v2 response envelope.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the JSON-RPC v2 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// AuthContextFunc extracts a requestor's opaque authorization-context token
// from an HTTP request — for example a verified bearer-token subject. It is the
// seam through which a deployment supplies requestor identity to the mount;
// returning "" means an unauthenticated requestor. A nil AuthContextFunc treats
// every request as unauthenticated.
type AuthContextFunc func(*http.Request) string

// Mount routes tasks/* JSON-RPC frames into an [Engine] ahead of the SDK
// server (RFC §8.2). It is a reusable artifact: one Mount is safe for
// concurrent use — HandleFrame and the HTTP middleware hold no per-call state.
type Mount struct {
	engine *Engine
	auth   AuthContextFunc
}

// NewMount constructs a Tasks transport mount over engine. engine must be
// non-nil.
func NewMount(engine *Engine) *Mount {
	return &Mount{engine: engine}
}

// WithAuthContext sets the function the HTTP middleware uses to derive a
// requestor's authorization context from a request, enabling auth-context
// binding of tasks/* over HTTP (RFC §8.5). It returns the Mount for chaining.
func (m *Mount) WithAuthContext(fn AuthContextFunc) *Mount {
	m.auth = fn
	return m
}

// IsTasksMethod reports whether method is one of the four tasks/* methods the
// mount intercepts.
func IsTasksMethod(method string) bool {
	switch method {
	case MethodGet, MethodResult, MethodCancel, MethodList:
		return true
	default:
		return false
	}
}

// HandleFrame serves one raw JSON-RPC request frame on behalf of the requestor
// identified by authContext. It returns (response, true, nil) when the frame
// was a tasks/* request the mount handled — response is the raw JSON-RPC
// response frame to write back. It returns (nil, false, nil) when the frame is
// not a tasks/* request and the caller must forward it to the SDK server. A
// non-nil error is a frame-decoding failure.
//
// A tasks/* notification (a frame with no id) is handled and yields an empty
// response — JSON-RPC notifications take no reply.
func (m *Mount) HandleFrame(
	ctx context.Context, authContext string, frame []byte,
) (response []byte, handled bool, err error) {
	var req jsonRPCRequest
	if err := json.Unmarshal(frame, &req); err != nil {
		return nil, false, fmt.Errorf("dockyard/runtime/tasks: decode JSON-RPC frame: %w", err)
	}
	if !IsTasksMethod(req.Method) {
		return nil, false, nil
	}
	result, dispatchErr := m.engine.DispatchAs(ctx, authContext, req.Method, req.Params)
	// A notification (no id) gets no response, even on error.
	if len(req.ID) == 0 || bytes.Equal(req.ID, []byte("null")) {
		return nil, true, nil
	}
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}
	if dispatchErr != nil {
		resp.Error = &jsonRPCError{
			Code:    JSONRPCCode(dispatchErr),
			Message: dispatchErr.Error(),
		}
	} else {
		resp.Result = result
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, true, fmt.Errorf("dockyard/runtime/tasks: encode JSON-RPC response: %w", err)
	}
	return out, true, nil
}

// HTTPMiddleware wraps next so a streamable-HTTP POST whose body is a single
// tasks/* JSON-RPC request is served by the Tasks engine and never reaches the
// SDK handler; every other request — including a non-tasks JSON-RPC frame and a
// GET (the SSE stream) — is forwarded to next untouched (RFC §8.2).
//
// The middleware reads and buffers the request body to inspect the method,
// then either answers directly or replays the buffered body to next, so the SDK
// handler sees an unconsumed body. A batch frame (a JSON array) is always
// forwarded — the mount intercepts only a single-request body.
func (m *Mount) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "read request body", http.StatusBadRequest)
			return
		}
		// Replay the buffered body for the forward path regardless of outcome.
		replay := func() { r.Body = io.NopCloser(bytes.NewReader(body)) }

		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || trimmed[0] == '[' {
			// Empty, or a JSON-RPC batch — not a single tasks/* request.
			replay()
			next.ServeHTTP(w, r)
			return
		}

		// An initialize request is forwarded to the SDK, but its response is
		// captured so the `capabilities.tasks` block — which the SDK has no
		// native field for — is merged in before it reaches the client.
		if peekMethod(trimmed) == methodInitialize {
			replay()
			m.serveInitialize(w, r, next)
			return
		}

		authCtx := ""
		if m.auth != nil {
			authCtx = m.auth(r)
		}
		resp, handled, err := m.HandleFrame(r.Context(), authCtx, trimmed)
		if err != nil || !handled {
			// Not a tasks/* frame, or undecodable — let the SDK handle it.
			replay()
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(resp) == 0 {
			// A tasks/* notification — no response body.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	})
}

// methodInitialize is the MCP handshake method whose response carries the
// server's capability block.
const methodInitialize = "initialize"

// peekMethod returns the `method` of a JSON-RPC request frame, or "" when the
// frame is not a decodable single request.
func peekMethod(frame []byte) string {
	var req jsonRPCRequest
	if err := json.Unmarshal(frame, &req); err != nil {
		return ""
	}
	return req.Method
}

// captureWriter buffers a handler's response so the mount can rewrite it. It
// records the status, headers, and body rather than streaming them through.
type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{header: http.Header{}, status: http.StatusOK}
}

func (c *captureWriter) Header() http.Header         { return c.header }
func (c *captureWriter) WriteHeader(status int)      { c.status = status }
func (c *captureWriter) Write(b []byte) (int, error) { return c.body.Write(b) }

// serveInitialize forwards an initialize request to next, captures the SDK's
// response, and merges the engine's `capabilities.tasks` block into the
// result's `capabilities` object before relaying it to the client. The SDK has
// no native capabilities.tasks field (the Tasks extension is experimental —
// RFC §8.2), so the mount injects it here; the value comes from the engine's
// codec (P3).
//
// The go-sdk's streamable-HTTP transport frames the initialize response in one
// of two ways: a plain `application/json` body, or a `text/event-stream` (SSE)
// body carrying the JSON-RPC response in a `data:` line. The mount handles
// BOTH — a real streamable-HTTP deployment uses SSE framing, so injecting only
// into the plain-JSON case would silently drop the capability on the wire
// (the wiring gap the Wave 5 checkpoint surfaced; see D-072). A response shape
// the mount does not recognise is relayed unchanged rather than corrupted.
func (m *Mount) serveInitialize(w http.ResponseWriter, r *http.Request, next http.Handler) {
	capW := newCaptureWriter()
	next.ServeHTTP(capW, r)

	relay := func(body []byte) {
		for k, vs := range capW.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(capW.status)
		_, _ = w.Write(body)
	}

	ct := capW.header.Get("Content-Type")
	body := capW.body.Bytes()
	if capW.status != http.StatusOK || ct == "" {
		relay(body)
		return
	}

	switch {
	case bytes.HasPrefix([]byte(ct), []byte("application/json")):
		merged, err := mergeTasksCapability(body, m.engine)
		if err != nil {
			// The result shape was unexpected — relay the SDK response as-is
			// rather than corrupt the handshake.
			relay(body)
			return
		}
		// The body length changed, so drop any stale Content-Length.
		capW.header.Del("Content-Length")
		relay(merged)
	case bytes.HasPrefix([]byte(ct), []byte("text/event-stream")):
		merged, err := mergeTasksCapabilitySSE(body, m.engine)
		if err != nil {
			relay(body)
			return
		}
		capW.header.Del("Content-Length")
		relay(merged)
	default:
		relay(body)
	}
}

// mergeTasksCapabilitySSE injects the engine's `capabilities.tasks` block into
// an SSE-framed initialize response. The go-sdk streamable-HTTP transport
// writes the JSON-RPC response as one SSE event — lines of `field: value`,
// blank-line-separated, the JSON-RPC envelope carried on the `data:` field.
// This rewrites the single `data:` payload that decodes as the initialize
// response and leaves every other line (event ids, comments, other events)
// untouched. It returns an error when no `data:` line carries an initialize
// response, so serveInitialize can relay the original body unchanged.
func mergeTasksCapabilitySSE(body []byte, e *Engine) ([]byte, error) {
	lines := bytes.Split(body, []byte("\n"))
	merged := false
	for i, line := range lines {
		// An SSE data field is `data:` optionally followed by a space then the
		// value; a continuation line is rare for a single-line JSON payload.
		const prefix = "data:"
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		payload := bytes.TrimPrefix(line, []byte(prefix))
		payload = bytes.TrimPrefix(payload, []byte(" "))
		trimmed := bytes.TrimSpace(payload)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			continue
		}
		rewritten, err := mergeTasksCapability(trimmed, e)
		if err != nil {
			// Not an initialize response — leave this data line as-is.
			continue
		}
		lines[i] = append([]byte("data: "), rewritten...)
		merged = true
	}
	if !merged {
		return nil, errors.New("no initialize-response data line in the SSE body")
	}
	return bytes.Join(lines, []byte("\n")), nil
}

// mergeTasksCapability injects the engine's `capabilities.tasks` block into a
// JSON-RPC initialize response body. It returns an error when the body is not a
// JSON-RPC response carrying a `result.capabilities` object.
func mergeTasksCapability(body []byte, e *Engine) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	rawResult, ok := envelope["result"]
	if !ok {
		return nil, errors.New("initialize response has no result")
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, err
	}
	caps := map[string]json.RawMessage{}
	if rawCaps, ok := result["capabilities"]; ok {
		if err := json.Unmarshal(rawCaps, &caps); err != nil {
			return nil, err
		}
	}
	tasksCap, err := e.CapabilityJSON()
	if err != nil {
		return nil, err
	}
	caps["tasks"] = tasksCap
	capsBytes, err := json.Marshal(caps)
	if err != nil {
		return nil, err
	}
	result["capabilities"] = capsBytes
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	envelope["result"] = resultBytes
	return json.Marshal(envelope)
}

// CapabilityFrame returns the `capabilities.tasks` JSON block to merge into the
// initialize-handshake result so a real MCP client discovers the Tasks
// operations the server supports (RFC §8.2). The go-sdk has no native
// capabilities.tasks field, so a deployment merges this block into the
// initialize response itself; CapabilityFrame is the single source of the
// value, produced through the engine's codec (P3).
func (m *Mount) CapabilityFrame() (json.RawMessage, error) {
	return m.engine.CapabilityJSON()
}

// ServeStdioFrames pumps newline-delimited JSON-RPC frames between in and out,
// intercepting tasks/* requests and forwarding every other frame to forward.
// It is the stdio counterpart of HTTPMiddleware: the SDK's stdio transport
// reads its own pipe, so a Dockyard stdio deployment that wants tasks/* over
// stdio runs the SDK server on a forwarded pipe pair and this pump on the real
// one.
//
// forward receives a frame the mount did not handle and returns the response
// frame to write back (or nil for a notification). ServeStdioFrames runs until
// in reaches EOF or ctx is cancelled, then returns. It is safe to run on its
// own goroutine; writes to out are serialized.
func (m *Mount) ServeStdioFrames(
	ctx context.Context, in io.Reader, out io.Writer,
	forward func(ctx context.Context, frame []byte) ([]byte, error),
) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var writeMu sync.Mutex
	writeFrame := func(b []byte) error {
		if len(b) == 0 {
			return nil
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := out.Write(append(b, '\n')); err != nil {
			return err
		}
		return nil
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		frame := bytes.TrimSpace(scanner.Bytes())
		if len(frame) == 0 {
			continue
		}
		// Copy: scanner.Bytes() is reused on the next Scan.
		frame = append([]byte(nil), frame...)
		resp, handled, err := m.HandleFrame(ctx, "", frame)
		if err == nil && handled {
			if werr := writeFrame(resp); werr != nil {
				return werr
			}
			continue
		}
		// Not a tasks/* frame — forward to the SDK server.
		if forward == nil {
			continue
		}
		fwdResp, ferr := forward(ctx, frame)
		if ferr != nil {
			return ferr
		}
		if werr := writeFrame(fwdResp); werr != nil {
			return werr
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("dockyard/runtime/tasks: stdio frame pump: %w", err)
	}
	return nil
}
