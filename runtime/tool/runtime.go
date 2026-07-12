package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/runtime/server"
)

// ErrInvalidArguments is the sentinel for an edge argument-validation failure:
// incoming tool-call arguments that violate the tool's generated input schema.
// Callers branch with errors.Is(err, ErrInvalidArguments); the concrete error
// is an *ArgumentError carrying the offending tool's name and the detail.
var ErrInvalidArguments = errors.New("dockyard/runtime/tool: invalid tool arguments")

// ArgumentError is the typed error the handler runtime produces when incoming
// tool-call arguments fail validation against the tool's generated input JSON
// Schema, at the catalog edge — before the handler runs. It wraps
// ErrInvalidArguments. Producing a typed error here means an invalid argument
// is a precise Dockyard diagnostic, never a panic and never a vague failure
// (RFC §5, AGENTS.md §5/§13; D-044).
type ArgumentError struct {
	// Tool is the wire name of the tool whose call was rejected.
	Tool string
	// Detail is the underlying schema-validation failure, human-readable.
	Detail string
}

// Error implements error.
func (e *ArgumentError) Error() string {
	return fmt.Sprintf("dockyard/runtime/tool: tool %q rejected invalid arguments: %s", e.Tool, e.Detail)
}

// Unwrap returns ErrInvalidArguments so errors.Is(err, ErrInvalidArguments)
// reports true for any ArgumentError.
func (e *ArgumentError) Unwrap() error { return ErrInvalidArguments }

// handlerRuntime is the per-tool production handler runtime. It validates
// incoming arguments at the catalog edge, runs the typed handler, hardens the
// content/structuredContent split (RFC §6.3), and detects oversized or
// misrouted payloads. One handlerRuntime is created per Register call and is
// safe for concurrent tool calls: the resolved validator is read-only after
// construction and flag accumulation is mutex-guarded.
type handlerRuntime[In, Out any] struct {
	toolName    string
	handler     ContinuationHandler[In, Out]
	inValidator *jsonschema.Resolved // resolved generated input schema; may be nil
	sizeBudget  int

	mu    sync.Mutex
	flags []Flag
}

// newHandlerRuntime builds the handler runtime for one tool. inSchema is the
// generated input JSON Schema; it is resolved once here so per-call validation
// is a pure read. A nil inSchema disables edge validation (the SDK still
// decodes); a schema that fails to resolve is a registration-time error so a
// misdeclared contract surfaces in Dockyard's own validation, not at runtime.
func newHandlerRuntime[In, Out any](
	toolName string,
	handler Handler[In, Out],
	inSchema *jsonschema.Schema,
	sizeBudget int,
) (*handlerRuntime[In, Out], error) {
	return newContinuationHandlerRuntime(toolName, func(ctx context.Context, call Call[In]) (Result[Out], error) {
		return handler(ctx, call.Input)
	}, inSchema, sizeBudget)
}

func newContinuationHandlerRuntime[In, Out any](
	toolName string,
	handler ContinuationHandler[In, Out],
	inSchema *jsonschema.Schema,
	sizeBudget int,
) (*handlerRuntime[In, Out], error) {
	rt := &handlerRuntime[In, Out]{
		toolName:   toolName,
		handler:    handler,
		sizeBudget: sizeBudget,
	}
	if inSchema != nil {
		resolved, err := inSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
		if err != nil {
			return nil, fmt.Errorf(
				"dockyard/runtime/tool: tool %q input schema cannot be resolved for edge validation: %w",
				toolName, err)
		}
		rt.inValidator = resolved
	}
	return rt, nil
}

// serve is the server.ToolOutputFunc the runtime installs. The SDK has already
// decoded the wire arguments into the typed In before calling serve; the
// runtime then (1) validates the arguments against the generated input schema
// at the catalog edge and rejects a violation with a typed *ArgumentError
// before the handler runs, (2) runs the handler, (3) detects oversized or
// misrouted payloads and records them as flags.
//
// The content/structuredContent split itself is enforced by server's
// AddToolWithSchemas: Text -> content[], Structured -> structuredContent, with
// no empty TextContent block when Text is empty (D-043). serve just supplies
// the typed ToolOutput.
func (rt *handlerRuntime[In, Out]) serve(ctx context.Context, call server.ToolCall[In]) (server.ToolOutput[Out], error) {
	if err := rt.validateArgs(ctx, call.Input); err != nil {
		return server.ToolOutput[Out]{}, err
	}

	res, err := rt.handler(ctx, Call[In]{Input: call.Input, InputResponses: call.InputResponses, RequestState: call.RequestState})
	if err != nil {
		return server.ToolOutput[Out]{}, err
	}

	rt.flagResult(res.Text, res.Structured)

	return server.ToolOutput[Out]{
		Text:          res.Text,
		Structured:    res.Structured,
		Meta:          res.Meta,
		InputRequests: res.InputRequests,
		RequestState:  res.RequestState,
		CreatedTask:   res.CreatedTask,
	}, nil
}

// validateArgs validates incoming tool-call arguments against the generated
// input schema at the catalog edge. It returns a typed *ArgumentError on a
// violation and nil when edge validation is disabled (no resolved validator)
// or passes.
//
// The raw wire arguments are validated when available (server.RawArguments) —
// validating the JSON catches violations that do not survive Go's decode, such
// as a missing required field (a non-pointer struct field always decodes to
// its zero value) or a type mismatch. When no raw arguments are present (a
// non-handler context — an in-process invocation), validateArgs re-serializes
// the decoded value to JSON and validates that, which still catches constraint
// violations on the values that are present. Either way the validator sees
// JSON-shaped data: it does not validate Go structs directly.
func (rt *handlerRuntime[In, Out]) validateArgs(ctx context.Context, in In) error {
	if rt.inValidator == nil {
		return nil
	}
	raw := server.RawArguments(ctx)
	if len(raw) == 0 {
		// No raw arguments (a non-handler context): fall back to the decoded
		// value, re-serialized to JSON. The validator validates JSON-shaped
		// data, not Go structs directly (jsonschema-go issue #23).
		b, err := json.Marshal(in)
		if err != nil {
			return &ArgumentError{Tool: rt.toolName, Detail: "arguments cannot be serialized: " + err.Error()}
		}
		raw = b
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

// flagResult detects oversized or misrouted payloads in one call's result and
// appends any flags raised. It never fails the call. Serializing Structured
// here mirrors how the SDK serializes structuredContent over the wire, so the
// flagged size is the size the model context would actually carry.
func (rt *handlerRuntime[In, Out]) flagResult(text string, structured Out) {
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

// snapshotFlags returns a copy of the flags raised so far, newest last, or nil
// when none have been raised. Safe for concurrent callers; the returned slice
// is the caller's to retain.
func (rt *handlerRuntime[In, Out]) snapshotFlags() []Flag {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.flags) == 0 {
		return nil
	}
	out := make([]Flag, len(rt.flags))
	copy(out, rt.flags)
	return out
}
