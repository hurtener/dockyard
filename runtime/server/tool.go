package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolFunc is a Dockyard tool handler. It is generic over a typed input and a
// typed output struct — the contract-first shape (RFC §6, P1): the SDK infers
// the JSON Schema from In and Out and validates incoming arguments before the
// handler runs. The full contract-first builder (app.Tool(...).Input[T]()...)
// lands in Phase 04; Phase 01 ships the minimal typed registration it sits on.
//
// A handler returns its typed output and an error. Returning a non-nil error
// surfaces as an MCP tool error to the host; a handler must never panic across
// the MCP boundary (AGENTS.md §5, §13).
type ToolFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// ToolDef describes a tool to register. Name is required; Description is a
// hint surfaced to the model.
type ToolDef struct {
	Name        string
	Description string
}

// ToolOutput is the result of a contract-first tool handler. It splits the two
// channels of an MCP CallToolResult (RFC §6.3): Text is model-facing and lands
// in content[]; Structured is the typed, UI-facing payload and lands in
// structuredContent; Meta lands in _meta.
//
// It is the seam the contract-first tool builder (runtime/tool, Phase 04) uses
// so the builder controls the content/structuredContent routing without
// reaching past the runtime into the raw SDK result type (P3 — the runtime
// surface does not expose raw protocol structs).
type ToolOutput[Out any] struct {
	Text       string
	Structured Out
	Meta       map[string]any
}

// ToolOutputFunc is a tool handler that returns the full ToolOutput split.
type ToolOutputFunc[In, Out any] func(ctx context.Context, in In) (ToolOutput[Out], error)

// AddTool registers a typed tool on the server. It must be called before Run.
// In and Out must each be a struct (or map) so the inferred input schema has
// JSON type "object", as the MCP spec requires.
//
// AddTool is a package function rather than a method because Go does not allow
// type parameters on methods; this mirrors the SDK's own mcp.AddTool.
func AddTool[In, Out any](s *Server, def ToolDef, fn ToolFunc[In, Out]) error {
	if s == nil {
		return errors.New("dockyard/runtime/server: AddTool on nil server")
	}
	if def.Name == "" {
		return errors.New("dockyard/runtime/server: ToolDef.Name is required")
	}
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: tool %q has a nil handler", def.Name)
	}
	for _, existing := range s.tools {
		if existing == def.Name {
			return fmt.Errorf("dockyard/runtime/server: tool %q already registered", def.Name)
		}
	}

	// Adapt the Dockyard handler to the SDK's ToolHandlerFor shape. The SDK
	// auto-populates CallToolResult.Content with JSON text of the typed Out
	// and sets StructuredContent; Phase 08 refines the content split (RFC §6.3).
	handler := func(ctx context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return nil, out, nil
	}

	// mcp.AddTool panics only on a schema-inference failure (e.g. a non-object
	// In/Out type). Recover so a misdeclared contract surfaces as a Dockyard
	// error, never a panic across the boundary (AGENTS.md §13).
	if err := addToolSafe(s.mcp, &mcpsdk.Tool{
		Name:        def.Name,
		Description: def.Description,
	}, handler); err != nil {
		return fmt.Errorf("dockyard/runtime/server: register tool %q: %w", def.Name, err)
	}

	s.tools = append(s.tools, def.Name)
	return nil
}

// AddToolWithSchemas registers a typed tool whose input and output JSON Schemas
// are supplied by the caller rather than inferred by the SDK at registration
// time. It is the seam the contract-first tool builder (runtime/tool, Phase 04)
// composes: the builder generates the schema from the Go contract struct via
// internal/codegen and hands it here, so the registered tool's schema is
// guaranteed to be the generated schema — the contract-first guarantee (P1,
// RFC §6.1), not whatever the SDK would infer separately.
//
// The handler returns a ToolOutput, so the builder controls the
// content/structuredContent split (RFC §6.3): ToolOutput.Text lands in
// content[], ToolOutput.Structured in structuredContent, ToolOutput.Meta in
// _meta.
//
// Either schema may be nil, in which case the SDK falls back to inferring it
// from In/Out (the same behaviour as AddTool). When non-nil, a schema must have
// JSON type "object" — the MCP spec's requirement for tool input/output schemas.
//
// In and Out must still be structs (or maps) so the SDK can decode arguments
// into In and encode Out into structuredContent.
func AddToolWithSchemas[In, Out any](
	s *Server,
	def ToolDef,
	in, out *jsonschema.Schema,
	fn ToolOutputFunc[In, Out],
) error {
	if s == nil {
		return errors.New("dockyard/runtime/server: AddToolWithSchemas on nil server")
	}
	if def.Name == "" {
		return errors.New("dockyard/runtime/server: ToolDef.Name is required")
	}
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: tool %q has a nil handler", def.Name)
	}
	for _, existing := range s.tools {
		if existing == def.Name {
			return fmt.Errorf("dockyard/runtime/server: tool %q already registered", def.Name)
		}
	}

	handler := func(ctx context.Context, _ *mcpsdk.CallToolRequest, arg In) (*mcpsdk.CallToolResult, Out, error) {
		out, err := fn(ctx, arg)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		// Populate Content explicitly so the model-facing text is the
		// handler's Text — the SDK only auto-fills Content with the JSON of
		// the output when Content is left unset (RFC §6.3).
		res := &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: out.Text}},
		}
		if len(out.Meta) > 0 {
			res.Meta = mcpsdk.Meta(out.Meta)
		}
		return res, out.Structured, nil
	}

	tool := &mcpsdk.Tool{Name: def.Name, Description: def.Description}
	if in != nil {
		tool.InputSchema = in
	}
	if out != nil {
		tool.OutputSchema = out
	}

	if err := addToolSafe(s.mcp, tool, handler); err != nil {
		return fmt.Errorf("dockyard/runtime/server: register tool %q: %w", def.Name, err)
	}

	s.tools = append(s.tools, def.Name)
	return nil
}

func addToolSafe[In, Out any](
	m *mcpsdk.Server,
	t *mcpsdk.Tool,
	h mcpsdk.ToolHandlerFor[In, Out],
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("schema inference failed: %v", r)
		}
	}()
	mcpsdk.AddTool(m, t, h)
	return nil
}
