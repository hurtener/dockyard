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
	// Meta is the tool definition's `_meta` object — the metadata a host sees
	// in tools/list, distinct from a CallToolResult's `_meta`. The Apps layer
	// (runtime/apps, Phase 09) supplies `_meta.ui` here to link a tool to its
	// ui:// resource (RFC §7.1). The map is opaque wire metadata built through
	// internal/protocolcodec; the runtime copies it verbatim onto the
	// registered tool and never inspects it (P3, RFC §5.4). A nil map leaves
	// the tool with no `_meta`.
	Meta map[string]any
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

// rawArgsKey is the unexported context key under which AddToolWithSchemas
// stashes the raw, undecoded tool-call arguments for the duration of a handler
// invocation.
type rawArgsKey struct{}

// RawArguments returns the raw, undecoded JSON arguments of the in-flight
// tool call, or nil if none are available (the call carried no arguments, or
// ctx is not a tool-handler context).
//
// It is the seam the contract-first handler runtime (runtime/tool, Phase 08)
// uses to validate incoming arguments against the tool's generated input JSON
// Schema *at the catalog edge* — before the typed handler runs — so a
// schema-violating argument becomes a typed Dockyard error rather than a vague
// failure (RFC §5, §6.3). The returned slice is the handler's to read, not to
// retain past the call.
func RawArguments(ctx context.Context) json.RawMessage {
	v, _ := ctx.Value(rawArgsKey{}).(json.RawMessage)
	return v
}

// WithRawArguments returns a copy of ctx carrying raw, undecoded tool-call
// arguments retrievable via RawArguments. AddToolWithSchemas calls it on every
// tool-handler invocation; it is also exported so an in-process invoker of the
// handler runtime — the inspector, a contract test — can drive edge validation
// without an over-the-wire call. Passing nil or empty args leaves ctx
// unchanged.
func WithRawArguments(ctx context.Context, raw json.RawMessage) context.Context {
	if len(raw) == 0 {
		return ctx
	}
	return context.WithValue(ctx, rawArgsKey{}, raw)
}

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
	//
	// The handler invocation is wrapped in guardHandler: an app author's
	// handler that panics on a live tools/call becomes a typed error result,
	// never a process crash — the "no panic across the MCP boundary" rule made
	// a toolchain guarantee (AGENTS.md §5, §13; D-053).
	handler := func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
		// Thread the in-flight MCP ServerSession onto the handler context so the
		// MCP logging → obs/v1 bridge (Phase 16, RFC §11.3) can reach it without
		// the typed handler signature exposing a raw SDK session (P3).
		ctx = withRequestSession(ctx, req)
		// Emit the obs/v1 tool.call lifecycle (RFC §11.2, P2). The end event
		// carries the shape+size capture of input/output — full content only
		// under an opted-in, redaction-aware policy (CLAUDE.md §7).
		endObs := s.rec.ToolCall(ctx, obs.NewTrace(), def.Name, toolTransport(req))
		var out Out
		err := guardHandler(ctx, s.log, "tool", def.Name, func() error {
			var herr error
			out, herr = fn(ctx, in)
			return herr
		})
		endObs(toolArgs(req), marshalForObs(out), err)
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
		Meta:        cloneMeta(def.Meta),
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

	handler := func(ctx context.Context, req *mcpsdk.CallToolRequest, arg In) (*mcpsdk.CallToolResult, Out, error) {
		// Stash the raw, undecoded arguments so a handler-runtime layer can
		// validate them against the generated input schema at the catalog
		// edge (RawArguments; Phase 08).
		if req != nil && req.Params != nil {
			ctx = WithRawArguments(ctx, req.Params.Arguments)
		}
		// Thread the in-flight MCP ServerSession onto the handler context so the
		// MCP logging → obs/v1 bridge (Phase 16, RFC §11.3) can reach it without
		// the typed handler signature exposing a raw SDK session (P3).
		ctx = withRequestSession(ctx, req)
		// Emit the obs/v1 tool.call lifecycle (RFC §11.2, P2).
		endObs := s.rec.ToolCall(ctx, obs.NewTrace(), def.Name, toolTransport(req))
		// guardHandler converts a panic in the app author's handler into a
		// typed error result — the server survives a panicking tool on a live
		// tools/call (AGENTS.md §5, §13; D-053).
		var out ToolOutput[Out]
		err := guardHandler(ctx, s.log, "tool", def.Name, func() error {
			var herr error
			out, herr = fn(ctx, arg)
			return herr
		})
		endObs(toolArgs(req), marshalForObs(out.Structured), err)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		// Populate Content explicitly so the model-facing text is the
		// handler's Text — the SDK only auto-fills Content with the JSON of
		// the output when Content is left unset (RFC §6.3).
		//
		// When the handler returns no model-facing text, emit a *non-nil but
		// empty* Content slice rather than a TextContent block holding an empty
		// string. A non-nil empty slice still suppresses the SDK's auto-fill of
		// the output JSON into content[] (the SDK only auto-fills when Content
		// is nil), so no UI-shaped payload leaks into the model context — and
		// no empty TextContent block is emitted either (D-043, the Wave 2 audit
		// quirk). A non-empty Text yields exactly one TextContent block.
		res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{}}
		if out.Text != "" {
			res.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: out.Text}}
		}
		if len(out.Meta) > 0 {
			res.Meta = mcpsdk.Meta(out.Meta)
		}
		return res, out.Structured, nil
	}

	tool := &mcpsdk.Tool{
		Name:        def.Name,
		Description: def.Description,
		Meta:        cloneMeta(def.Meta),
	}
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

// cloneMeta returns a shallow copy of m, or nil if m is nil/empty. Registration
// copies a caller's `_meta` map so a later mutation of the caller's map cannot
// reach the registered tool or resource.
func cloneMeta(m map[string]any) mcpsdk.Meta {
	if len(m) == 0 {
		return nil
	}
	out := make(mcpsdk.Meta, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
