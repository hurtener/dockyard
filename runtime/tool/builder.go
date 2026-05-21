package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/server"
)

// Handler is a contract-first tool handler: it receives the typed, decoded and
// schema-validated input and returns a typed Result. Returning a non-nil error
// surfaces as an MCP tool error to the host; a handler must never panic across
// the MCP boundary (AGENTS.md §5, §13).
type Handler[In, Out any] func(ctx context.Context, in In) (Result[Out], error)

// Builder declares a single MCP tool in the contract-first style (RFC §6). The
// input and output contract types are bound by New; the fluent methods set the
// remaining metadata; Register generates the schema and installs the tool on a
// server. A Builder is not safe for concurrent use — build a tool, register it,
// then discard the Builder; independent Builders on independent servers may run
// concurrently.
type Builder[In, Out any] struct {
	name        string
	description string
	uiResource  string
	handler     Handler[In, Out]

	// runtime is the per-tool handler runtime, created by Register. It is the
	// seam Flags reads. nil before Register.
	runtime *handlerRuntime[In, Out]
}

// New starts a contract-first tool declaration. In is the tool's input contract
// type and Out its output contract type — both must be object types (a struct
// or a string-keyed map), the MCP requirement for tool schemas. name is the
// tool's wire name and is required.
//
// The type parameters are bound here, at construction, rather than by fluent
// .Input[T]()/.Output[T]() methods, because Go does not permit type parameters
// on methods (D-029).
func New[In, Out any](name string) *Builder[In, Out] {
	return &Builder[In, Out]{name: name}
}

// Describe sets the tool description — the hint surfaced to the model.
func (b *Builder[In, Out]) Describe(desc string) *Builder[In, Out] {
	b.description = desc
	return b
}

// UI associates the tool with a ui:// resource by its convention name (the
// .svelte file stem). Phase 04 records the association; the Apps layer (Phase
// 09) consumes it to wire _meta.ui. Recording it on the builder keeps the
// tool-to-UI mapping explicit rather than hidden behind file-drop convention
// alone (brief 04 §4).
func (b *Builder[In, Out]) UI(resourceName string) *Builder[In, Out] {
	b.uiResource = resourceName
	return b
}

// Handler sets the tool's handler.
func (b *Builder[In, Out]) Handler(h Handler[In, Out]) *Builder[In, Out] {
	b.handler = h
	return b
}

// UIResource reports the ui:// resource name set by UI, or "" if none. It is the
// read seam Phase 06's manifest and Phase 09's Apps layer use to discover the
// tool-to-UI wiring.
func (b *Builder[In, Out]) UIResource() string { return b.uiResource }

// Name reports the tool's wire name.
func (b *Builder[In, Out]) Name() string { return b.name }

// Schemas returns the generated input and output JSON Schemas for the tool's
// contract types, without registering anything. It is exported so the manifest
// (Phase 06) and the validate command (Phase 18) can obtain a tool's schema
// without a server. The returned schemas are the same ones Register installs.
func (b *Builder[In, Out]) Schemas() (in, out *jsonschema.Schema, err error) {
	in, err = codegen.SchemaFor[In]()
	if err != nil {
		return nil, nil, fmt.Errorf("dockyard/runtime/tool: tool %q input contract: %w", b.name, err)
	}
	out, err = codegen.SchemaFor[Out]()
	if err != nil {
		return nil, nil, fmt.Errorf("dockyard/runtime/tool: tool %q output contract: %w", b.name, err)
	}
	return in, out, nil
}

// Register generates the tool's JSON Schema from its contract types and
// installs the tool on s. The registered tool's input and output schema is the
// generated schema — that is the contract-first guarantee (P1, RFC §6.1).
//
// Register validates the builder is complete and the contract types are valid;
// it returns a typed error rather than panicking on any misuse.
func (b *Builder[In, Out]) Register(s *server.Server) error {
	if s == nil {
		return errors.New("dockyard/runtime/tool: Register on nil server")
	}
	if b.name == "" {
		return errors.New("dockyard/runtime/tool: tool name is required")
	}
	if b.handler == nil {
		return fmt.Errorf("dockyard/runtime/tool: tool %q has no handler", b.name)
	}

	in, out, err := b.Schemas()
	if err != nil {
		return err
	}

	// Build the production handler runtime (Phase 08): it validates incoming
	// arguments against the generated input schema at the catalog edge, runs
	// the handler, and flags oversized or misrouted payloads (RFC §5, §6.3).
	rt, err := newHandlerRuntime(b.name, b.handler, in, DefaultOutputSizeBudget)
	if err != nil {
		return err
	}
	b.runtime = rt

	def := server.ToolDef{Name: b.name, Description: b.description}
	if err := server.AddToolWithSchemas(s, def, in, out, rt.serve); err != nil {
		return fmt.Errorf("dockyard/runtime/tool: register tool %q: %w", b.name, err)
	}
	return nil
}

// Flags reports the routing flags — oversized outputs, misrouted UI payloads —
// raised by this tool's handler since Register, newest last (RFC §6.3; D-045).
// A flag is non-fatal: it never failed a tool call, it is recorded for
// inspection. The returned slice is a copy and safe for the caller to retain.
// Flags is safe to call concurrently with in-flight tool calls. It returns nil
// before Register installs the handler runtime.
func (b *Builder[In, Out]) Flags() []Flag {
	if b.runtime == nil {
		return nil
	}
	return b.runtime.snapshotFlags()
}
