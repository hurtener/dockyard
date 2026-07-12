package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// invokeTimeout bounds one operator-initiated tools/call — a connect, the
// tools/call, and a result read. The inspector is a dev surface; an attached
// server that does not answer promptly surfaces as a typed error rather than
// stalling the UI.
const invokeTimeout = 30 * time.Second

// InvokeRequest is the operator's typed request to `POST /api/tools/invoke`.
// The body is small on purpose: a tool name and a JSON-object arguments value.
// The inspector frontend builds Arguments from the tool's generated input JSON
// Schema (P1 — the schema is the source of truth for the form's shape).
type InvokeRequest struct {
	// Tool is the registered tool name to call. Required.
	Tool string `json:"tool"`
	// Arguments is the typed input the handler receives. The wire shape is
	// json.RawMessage so the inspector never decodes user input into a runtime
	// struct (P3 — no raw protocol struct leaks; the runtime/server schema
	// validates the payload at the catalog edge before the handler runs).
	Arguments json.RawMessage `json:"arguments"`
	// InputResponses and RequestState retry a modern core MRTR tools/call.
	InputResponses mcpsdk.InputResponseMap `json:"inputResponses,omitempty"`
	RequestState   string                  `json:"requestState,omitempty"`
}

// InvokeResponse is the JSON the inspector returns from a successful
// `POST /api/tools/invoke`. Content carries the model-facing text parts; the
// inspector frontend reads only StructuredContent for its App preview render
// (the same path the Fixtures switcher's pushToolResult flows through —
// D-129). IsError mirrors the MCP CallToolResult.isError flag: a tool that
// returned a typed error to the host is still a successful RPC, surfaced here
// so the inspector can render the error state without conflating it with a
// transport-level failure.
//
// This is the inspector's own type — no raw MCP SDK struct leaks through it
// (P3, mirroring [AppPreview] and [Verdict]).
type InvokeResponse struct {
	// Content is the MCP CallToolResult content[]: text and any non-structured
	// content parts the tool emitted. Marshalled as JSON so the inspector can
	// surface a faithful view in its result viewer.
	Content json.RawMessage `json:"content,omitempty"`
	// StructuredContent is the tool's typed structured payload — the value the
	// App-frame's pushToolResult path consumes. May be omitted (a tool that
	// emits only text content).
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	// IsError is the MCP CallToolResult.isError flag: a tool-level error
	// reported to the host rather than a protocol-level error.
	IsError bool `json:"isError,omitempty"`
	// InputRequests and RequestState are returned when modern core MRTR needs
	// operator input. The caller retries with keyed InputResponses.
	InputRequests mcpsdk.InputRequestMap `json:"inputRequests,omitempty"`
	RequestState  string                 `json:"requestState,omitempty"`
}

// ToolInvoker performs one operator-initiated tools/call against the attached
// MCP server and returns the result. The inspector calls it per
// `POST /api/tools/invoke` request. It is the lone mutating surface in the
// inspector backend — gated by the operator's UI action and by the inspector's
// localhost-only bind (RFC §12, P4; D-131 extends D-099 + D-103 to add
// operator-initiated tools/call to the inspector's read-only attach).
//
// The Arguments is the raw JSON object the operator supplied; the
// runtime/server schema validates it at the catalog edge before the typed
// handler runs (P1 — the generated input JSON Schema is the source of truth).
type ToolInvoker func(ctx context.Context, req InvokeRequest) (*InvokeResponse, error)

// ToolsFromServer adapts a running MCP server, named by its base URL, into a
// [ToolInvoker]. It is the operator-initiated tools/call path D-131 makes
// binding: the inspector additionally issues real tools/call to the attached
// server when an operator initiates it through the UI. This stays within P4:
//
//   - The inspector remains the lone client-shaped surface, dev-mode-gated,
//     localhost-only (the listener's `requireLoopback` gate already enforces
//     this — see [requireLoopback]).
//   - Each invocation opens a short-lived client session, calls one tool,
//     and closes — no long-lived production client.
//   - The operator is the one driving the write through the UI, not an
//     off-localhost actor; symmetric to D-103's read-only resources/read for
//     App rendering.
//
// A nil baseURL yields a source that returns an error — without an attached
// server there is no tool to call, and the inspector frontend surfaces the
// error in its result region.
func ToolsFromServer(baseURL string) ToolInvoker {
	return func(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
		if baseURL == "" {
			return nil, errors.New(
				"dockyard/internal/inspector: inspector is detached — " +
					"no server URL to call tools against")
		}
		return invokeAttachedTool(ctx, baseURL, req)
	}
}

// invokeAttachedTool connects a short-lived MCP client to baseURL, calls one
// tool, and closes the session before return. A connect or call failure is
// returned as a typed error; the `/api/tools/invoke` handler maps it to an
// HTTP 502 with a JSON message body so the inspector frontend surfaces an
// honest error state. A tool that reports a *tool-level* error (sets
// `isError` on its CallToolResult) is a successful RPC — the response's
// IsError flag carries it through, the HTTP status stays 200.
func invokeAttachedTool(
	ctx context.Context,
	baseURL string,
	req InvokeRequest,
) (*InvokeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()

	// The arguments wire field is a JSON object. The SDK accepts any value
	// that marshals to JSON; we pass the raw bytes so the inspector never
	// decodes user-supplied input into a runtime struct (P3) — the runtime's
	// schema validation runs at the catalog edge before the handler runs.
	// An empty body is tolerated: a tool with no required inputs is callable
	// with `arguments: {}`.
	args := req.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-inspector", Version: "0.1.0"},
		&mcpsdk.ClientOptions{
			// Inspector responses are operator-driven and arrive in a later HTTP
			// request. Do not let the SDK synchronously fabricate responses.
			MultiRoundTrip: &mcpsdk.MultiRoundTripOptions{Disabled: true},
		},
	)
	session, err := client.Connect(ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: baseURL}, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: connect %q: %w", baseURL, err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:           req.Tool,
		Arguments:      args,
		InputResponses: req.InputResponses,
		RequestState:   req.RequestState,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: tools/call %q: %w", req.Tool, err)
	}
	if result == nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: tools/call %q returned no result", req.Tool)
	}

	resp := &InvokeResponse{
		IsError: result.IsError, InputRequests: result.InputRequests, RequestState: result.RequestState,
	}
	if len(result.Content) > 0 {
		contentJSON, mErr := json.Marshal(result.Content)
		if mErr != nil {
			return nil, fmt.Errorf(
				"dockyard/internal/inspector: marshal tools/call content: %w", mErr)
		}
		resp.Content = contentJSON
	}
	if result.StructuredContent != nil {
		structJSON, mErr := json.Marshal(result.StructuredContent)
		if mErr != nil {
			return nil, fmt.Errorf(
				"dockyard/internal/inspector: marshal tools/call structuredContent: %w", mErr)
		}
		resp.StructuredContent = structJSON
	}
	return resp, nil
}
