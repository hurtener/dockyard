package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
)

// ContentToolOutput is the complete result of a non-MRTR contract-first tool
// handler. It is deliberately separate from ToolOutput: a content tool can
// carry standard MCP non-text blocks, while its type does not expose
// InputRequests or RequestState and therefore cannot accidentally produce an
// invalid content-plus-continuation result.
type ContentToolOutput[Out any] struct {
	// Text is model-facing text and is emitted before Content when non-empty.
	Text string
	// Content contains standard MCP content blocks in caller-supplied order.
	Content []mcpsdk.Content
	// Structured is the typed, UI-facing output rendered into structuredContent.
	Structured Out
	// StructuredPresent forces structuredContent to be emitted when Structured
	// is a typed nil value.
	StructuredPresent bool
	// Meta is optional extension metadata rendered into _meta.
	Meta map[string]any
}

// ContentToolOutputFunc is the handler shape for AddContentToolWithSchemas.
// It is intentionally non-continuable; use AddToolWithSchemasMRTR for a tool
// that needs multi-round-trip input requests.
type ContentToolOutputFunc[In, Out any] func(context.Context, In) (ContentToolOutput[Out], error)

// ErrInvalidContent is the sentinel for content blocks that are not valid in a
// server tools/call result. ContentError wraps it with the block index and
// concrete type.
var ErrInvalidContent = errors.New("dockyard/runtime/server: invalid tools/call content")

// ContentError identifies one invalid block in a ContentToolOutput. The
// official MCP SDK exposes sampling-only content types through the same
// interface, so the server edge validates the closed tools/call family before
// handing a result to the SDK.
type ContentError struct {
	Index int
	Type  string
	Why   string
}

// Error implements error.
func (e *ContentError) Error() string {
	return fmt.Sprintf("dockyard/runtime/server: invalid tools/call content[%d] (%s): %s", e.Index, e.Type, e.Why)
}

// Unwrap returns ErrInvalidContent for errors.Is checks.
func (e *ContentError) Unwrap() error { return ErrInvalidContent }

// AddContentToolWithSchemas registers a complete-only contract-first tool
// whose input and output JSON Schemas are supplied by the caller. Text and
// standard MCP content blocks are emitted in deterministic text-first order;
// Structured and Meta retain their normal destinations.
//
// The accepted content family is exactly TextContent, ImageContent,
// AudioContent, ResourceLink, and EmbeddedResource. Sampling-only
// ToolUseContent/ToolResultContent, nil blocks, and unknown values are rejected
// as a tool error before the SDK serializes the result. This API has no
// continuation fields, so Content cannot be combined with InputRequests or
// RequestState by construction.
func AddContentToolWithSchemas[In, Out any](
	s *Server,
	def ToolDef,
	in, out *jsonschema.Schema,
	fn ContentToolOutputFunc[In, Out],
) error {
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: tool %q has a nil content handler", def.Name)
	}
	if s == nil {
		return errors.New("dockyard/runtime/server: AddContentToolWithSchemas on nil server")
	}
	if def.Name == "" {
		return errors.New("dockyard/runtime/server: ToolDef.Name is required")
	}
	for _, existing := range s.tools {
		if existing == def.Name {
			return fmt.Errorf("dockyard/runtime/server: tool %q already registered", def.Name)
		}
	}

	handler := func(ctx context.Context, req *mcpsdk.CallToolRequest, arg In) (*mcpsdk.CallToolResult, Out, error) {
		if req != nil && req.Params != nil {
			ctx = WithRawArguments(ctx, req.Params.Arguments)
			ctx = WithRequestMeta(ctx, req.Params.Meta)
		}
		ctx = withRequestSession(ctx, req)
		span := obs.NewTraceFromContext(ctx)
		ctx = obs.WithSpan(ctx, span)
		endObs := s.rec.ToolCall(ctx, span, def.Name, toolTransport(req))

		var result ContentToolOutput[Out]
		err := guardHandler(ctx, s.log, "tool", def.Name, func() error {
			var handlerErr error
			result, handlerErr = fn(ctx, arg)
			return handlerErr
		})
		if err == nil {
			err = validateContentBlocks(result.Content)
		}
		endObs(toolArgs(req), marshalForObs(result.Structured), err)
		if err != nil {
			var zero Out
			return nil, zero, err
		}

		// A non-nil empty slice suppresses the SDK's automatic duplicate JSON
		// text for object structured output, preserving the established Dockyard
		// content/structuredContent split.
		content := make([]mcpsdk.Content, 0, len(result.Content)+1)
		if result.Text != "" {
			content = append(content, &mcpsdk.TextContent{Text: result.Text})
		}
		content = append(content, result.Content...)
		res := &mcpsdk.CallToolResult{Content: content}
		present, explicitNull := structuredPresence(result.Structured, result.StructuredPresent)
		setStructuredPresence(ctx, present, explicitNull)
		if explicitNull {
			res.StructuredContent = json.RawMessage("null")
		}
		if len(result.Meta) > 0 {
			res.Meta = cloneMeta(result.Meta)
		}
		return res, result.Structured, nil
	}

	t := &mcpsdk.Tool{
		Name:        def.Name,
		Description: def.Description,
		Meta:        cloneMeta(def.Meta),
	}
	if in != nil {
		t.InputSchema = in
	}
	if out != nil {
		t.OutputSchema = out
	}
	if err := addToolSafe(s.mcp, t, handler); err != nil {
		return fmt.Errorf("dockyard/runtime/server: register content tool %q: %w", def.Name, err)
	}
	s.tools = append(s.tools, def.Name)
	return nil
}

func validateContentBlocks(content []mcpsdk.Content) error {
	for i, block := range content {
		if block == nil {
			return &ContentError{Index: i, Type: "<nil>", Why: "nil blocks are not valid in tools/call content"}
		}
		switch c := block.(type) {
		case *mcpsdk.TextContent:
			if c == nil {
				return &ContentError{Index: i, Type: "*mcp.TextContent", Why: "nil blocks are not valid in tools/call content"}
			}
		case *mcpsdk.ImageContent:
			if c == nil {
				return &ContentError{Index: i, Type: "*mcp.ImageContent", Why: "nil blocks are not valid in tools/call content"}
			}
		case *mcpsdk.AudioContent:
			if c == nil {
				return &ContentError{Index: i, Type: "*mcp.AudioContent", Why: "nil blocks are not valid in tools/call content"}
			}
		case *mcpsdk.ResourceLink:
			if c == nil {
				return &ContentError{Index: i, Type: "*mcp.ResourceLink", Why: "nil blocks are not valid in tools/call content"}
			}
		case *mcpsdk.EmbeddedResource:
			if c == nil {
				return &ContentError{Index: i, Type: "*mcp.EmbeddedResource", Why: "nil blocks are not valid in tools/call content"}
			}
			if c.Resource == nil {
				return &ContentError{Index: i, Type: "*mcp.EmbeddedResource", Why: "resource must not be nil"}
			}
		default:
			return &ContentError{Index: i, Type: fmt.Sprintf("%T", block), Why: "type is not valid in a tools/call result"}
		}
	}
	return nil
}
