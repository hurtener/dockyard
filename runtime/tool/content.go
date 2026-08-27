package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
)

// ContentResult is the result of a complete-only contract-first tool handler.
// Text is emitted first in model-facing content[], followed by the standard
// MCP blocks in Content. Structured and Meta retain the same destinations as
// Result. ContentResult intentionally has no continuation fields: content
// tools cannot accidentally combine media blocks with MRTR input requests.
type ContentResult[Out any] struct {
	// Text is model-facing text emitted before Content when non-empty.
	Text string
	// Content contains standard MCP tools/call blocks in caller-supplied order.
	Content []mcpsdk.Content
	// Structured is the typed, UI-facing output rendered into structuredContent.
	Structured Out
	// StructuredPresent forces structuredContent to be emitted when Structured
	// is a typed nil value.
	StructuredPresent bool
	// Meta is optional extension metadata rendered into _meta.
	Meta map[string]any
}

// ContentHandler is the complete-only handler shape for NewContent. It has no
// Call or continuation state so the type system keeps standard content results
// separate from multi-round-trip tools.
type ContentHandler[In, Out any] func(context.Context, In) (ContentResult[Out], error)

// ContentBuilder declares a complete-only MCP tool whose result may contain
// standard MCP non-text content. It mirrors the contract-first metadata and
// schema surface of Builder while deliberately omitting ContinuationHandler.
// A ContentBuilder is not safe for concurrent use; build and register it once.
type ContentBuilder[In, Out any] struct {
	name         string
	description  string
	uiResource   string
	uiVisibility []string
	handler      ContentHandler[In, Out]
	runtime      *contentHandlerRuntime[In, Out]
}

// NewContent starts a complete-only contract-first content tool declaration.
// In must be an object contract; Out may be any JSON-representable Go type.
func NewContent[In, Out any](name string) *ContentBuilder[In, Out] {
	return &ContentBuilder[In, Out]{name: name}
}

// Describe sets the tool description surfaced to the model.
func (b *ContentBuilder[In, Out]) Describe(desc string) *ContentBuilder[In, Out] {
	b.description = desc
	return b
}

// UI associates the tool with a ui:// App resource and optional visibility.
func (b *ContentBuilder[In, Out]) UI(resourceName string, visibility ...string) *ContentBuilder[In, Out] {
	b.uiResource = resourceName
	b.uiVisibility = visibility
	return b
}

// Handler sets the complete-only typed content handler.
func (b *ContentBuilder[In, Out]) Handler(h ContentHandler[In, Out]) *ContentBuilder[In, Out] {
	b.handler = h
	return b
}

// UIResource reports the ui:// resource name set by UI, or "" if none.
func (b *ContentBuilder[In, Out]) UIResource() string { return b.uiResource }

// Name reports the tool's wire name.
func (b *ContentBuilder[In, Out]) Name() string { return b.name }

// Schemas returns the generated input and output JSON Schemas without
// registering the content tool.
func (b *ContentBuilder[In, Out]) Schemas() (in, out *jsonschema.Schema, err error) {
	in, err = codegen.SchemaFor[In]()
	if err != nil {
		return nil, nil, fmt.Errorf("dockyard/runtime/tool: content tool %q input contract: %w", b.name, err)
	}
	out, err = codegen.OutputSchemaFor[Out]()
	if err != nil {
		return nil, nil, fmt.Errorf("dockyard/runtime/tool: content tool %q output contract: %w", b.name, err)
	}
	return in, out, nil
}

// Register generates the content tool's schemas and installs it on s. The
// registration path is non-MRTR and validates the closed tools/call content
// family immediately before the SDK receives each result.
func (b *ContentBuilder[In, Out]) Register(s *server.Server) error {
	if s == nil {
		return errors.New("dockyard/runtime/tool: content Register on nil server")
	}
	if b.name == "" {
		return errors.New("dockyard/runtime/tool: content tool name is required")
	}
	if b.handler == nil {
		return fmt.Errorf("dockyard/runtime/tool: content tool %q has no handler", b.name)
	}
	in, out, err := b.Schemas()
	if err != nil {
		return err
	}
	rt, err := newContentHandlerRuntime(b.name, b.handler, in, DefaultOutputSizeBudget)
	if err != nil {
		return err
	}
	b.runtime = rt

	def := server.ToolDef{Name: b.name, Description: b.description}
	if b.uiResource != "" {
		link, ok := s.AppLinkByName(b.uiResource)
		if !ok {
			return fmt.Errorf("dockyard/runtime/tool: content tool %q: .UI(%q) references no registered App — "+
				"register the App with apps.Register before registering the tool", b.name, b.uiResource)
		}
		meta, err := apps.ToolMetaFor(apps.ToolLink{
			ResourceURI:           link.URI,
			Visibility:            b.uiVisibility,
			EmitLegacyResourceURI: s.EmitLegacyToolUIMeta(),
		})
		if err != nil {
			return fmt.Errorf("dockyard/runtime/tool: content tool %q: wire _meta.ui: %w", b.name, err)
		}
		def.Meta = meta
	}
	if err := server.AddContentToolWithSchemas(s, def, in, out, rt.serve); err != nil {
		return fmt.Errorf("dockyard/runtime/tool: register content tool %q: %w", b.name, err)
	}
	return nil
}

// Flags reports non-fatal routing flags raised by this content tool since
// registration, newest last. Standard content bytes are validated by the
// server seam; text and structured payloads use the same flag policy as Builder.
func (b *ContentBuilder[In, Out]) Flags() []Flag {
	if b.runtime == nil {
		return nil
	}
	return b.runtime.snapshotFlags()
}

type contentHandlerRuntime[In, Out any] struct {
	toolName    string
	handler     ContentHandler[In, Out]
	inValidator *jsonschema.Resolved
	sizeBudget  int

	mu    sync.Mutex
	flags []Flag
}

func newContentHandlerRuntime[In, Out any](
	toolName string,
	handler ContentHandler[In, Out],
	inSchema *jsonschema.Schema,
	sizeBudget int,
) (*contentHandlerRuntime[In, Out], error) {
	rt := &contentHandlerRuntime[In, Out]{
		toolName:   toolName,
		handler:    handler,
		sizeBudget: sizeBudget,
	}
	if inSchema != nil {
		resolved, err := inSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
		if err != nil {
			return nil, fmt.Errorf(
				"dockyard/runtime/tool: content tool %q input schema cannot be resolved for edge validation: %w",
				toolName, err)
		}
		rt.inValidator = resolved
	}
	return rt, nil
}

func (rt *contentHandlerRuntime[In, Out]) serve(ctx context.Context, in In) (server.ContentToolOutput[Out], error) {
	if err := rt.validateArgs(ctx, in); err != nil {
		return server.ContentToolOutput[Out]{}, err
	}
	res, err := rt.handler(ctx, in)
	if err != nil {
		return server.ContentToolOutput[Out]{}, err
	}
	rt.flagResult(res.Text, res.Structured)
	return server.ContentToolOutput[Out]{
		Text:              res.Text,
		Content:           res.Content,
		Structured:        res.Structured,
		StructuredPresent: res.StructuredPresent,
		Meta:              res.Meta,
	}, nil
}

func (rt *contentHandlerRuntime[In, Out]) validateArgs(ctx context.Context, in In) error {
	if rt.inValidator == nil {
		return nil
	}
	raw := server.RawArguments(ctx)
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(in)
		if err != nil {
			return &ArgumentError{Tool: rt.toolName, Detail: "arguments cannot be serialized: " + err.Error()}
		}
	}
	var instance any
	if err := json.Unmarshal(raw, &instance); err != nil {
		return &ArgumentError{Tool: rt.toolName, Detail: "arguments are not valid JSON: " + err.Error()}
	}
	if err := rt.inValidator.Validate(instance); err != nil {
		return &ArgumentError{Tool: rt.toolName, Detail: err.Error()}
	}
	return nil
}

func (rt *contentHandlerRuntime[In, Out]) flagResult(text string, structured Out) {
	var structuredJSON []byte
	if b, err := json.Marshal(structured); err == nil {
		structuredJSON = b
	}
	raised := detectFlags(rt.toolName, text, structuredJSON, rt.sizeBudget)
	if len(raised) == 0 {
		return
	}
	rt.mu.Lock()
	rt.flags = append(rt.flags, raised...)
	rt.mu.Unlock()
}

func (rt *contentHandlerRuntime[In, Out]) snapshotFlags() []Flag {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.flags) == 0 {
		return nil
	}
	out := make([]Flag, len(rt.flags))
	copy(out, rt.flags)
	return out
}
