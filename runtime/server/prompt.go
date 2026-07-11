package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
)

// PromptArgument describes one argument an MCP Prompt accepts.
//
// MCP prompts (RFC §6 — adjacent to tools and resources) carry a
// string-valued argument list rather than a typed input contract: the host
// renders the prompt template, fills the arguments from the user / model,
// and pulls the rendered messages back via prompts/get. Because the
// argument shape is a flat list of named strings (not a structured JSON
// object), Dockyard's P1 contract-first pattern — typed Go struct → JSON
// Schema — does not extend naturally to prompts (D-152). The PromptArgument
// shape mirrors mcp.PromptArgument verbatim so a developer reads the same
// fields the spec documents.
type PromptArgument struct {
	// Name is the argument's wire name.
	Name string
	// Title is the optional human-facing label.
	Title string
	// Description is the optional human-readable description.
	Description string
	// Required reports whether the host must supply this argument.
	Required bool
}

// PromptDef describes an MCP Prompt to register. Name is required.
type PromptDef struct {
	// Name is the prompt's wire identifier; required.
	Name string
	// Title is the optional human-facing display label.
	Title string
	// Description is an optional model-facing summary.
	Description string
	// Arguments is the prompt's argument schema. Optional — a prompt with
	// no arguments renders a fixed template.
	Arguments []PromptArgument
}

// PromptRequest is the arguments a host supplied with a prompts/get call.
// It is the Dockyard-facing view of mcp.GetPromptParams, so a handler
// signature never imports the raw SDK type (P3 — RFC §5.4).
type PromptRequest struct {
	// Name is the prompt name the host asked for.
	Name string
	// Arguments is the host-supplied argument map; nil when none were sent.
	// Values are always strings — the MCP spec scopes prompt arguments to
	// string values.
	Arguments map[string]string
}

// PromptMessage is one message in a rendered prompt — a role plus a text
// body. The wire shape carries richer Content variants (resource embeds,
// images, …); Dockyard exposes the text-only shape for V1 because the
// in-tree consumer (the inspector + a host that renders the prompt as a
// chat-message seed) needs nothing more, and the typed shape stays small
// and explainable. A future phase that needs richer content adds a
// dedicated variant rather than leaking the SDK's wireContent here (P3).
type PromptMessage struct {
	// Role is the message role: "user" | "assistant" | "system".
	Role string
	// Text is the message body. Empty text emits an empty TextContent
	// block, which the spec permits and the inspector renders as a blank
	// message.
	Text string
}

// PromptResult is what a prompts/get handler returns: an optional
// description plus the rendered messages.
type PromptResult struct {
	// Description is an optional rendered-description override. When empty
	// the registered PromptDef.Description is used.
	Description string
	// Messages are the rendered messages in the order they should be
	// presented. At least one message is the conventional shape, but the
	// spec does not require it; an empty Messages slice surfaces as an
	// empty `messages` array to the host.
	Messages []PromptMessage
}

// PromptHandler renders a prompt: the host's prompts/get call carries the
// request arguments, and the handler returns the rendered messages.
//
// A handler returns its typed result and an error. Returning a non-nil
// error surfaces as an MCP prompts/get error to the host; a handler must
// never panic across the MCP boundary (AGENTS.md §5, §13) — Dockyard
// recovers the panic and converts it into a typed Dockyard error.
type PromptHandler func(ctx context.Context, req PromptRequest) (PromptResult, error)

// AddPrompt registers an MCP Prompt on the server. It must be called
// before Run / ServeStdio / HTTPHandler. The runtime emits an obs/v1
// `prompt.get` lifecycle pair on every prompts/get invocation (RFC §11.2,
// P2) and recovers any handler panic so the server process survives a bad
// handler (AGENTS.md §5, §13).
//
// AddPrompt is a thin, focused pass-through to the SDK's prompt
// registration rather than a contract-first builder (D-152): MCP prompt
// arguments are typed as flat string maps, not structured objects, so the
// Go struct → JSON Schema pattern that backs AddTool / runtime/tool.New
// does not extend naturally. A Dockyard developer who needs a richer
// argument shape composes the prompt of multiple smaller prompts, or wraps
// the typed call in a contract-first tool that internally drives the
// prompt — the same idiom the MCP spec encourages.
//
// Registration is not safe for concurrent calls; a server is assembled
// once before Run, then served.
func AddPrompt(s *Server, def PromptDef, fn PromptHandler) error {
	if s == nil {
		return errors.New("dockyard/runtime/server: AddPrompt on nil server")
	}
	if def.Name == "" {
		return errors.New("dockyard/runtime/server: PromptDef.Name is required")
	}
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: prompt %q has a nil handler", def.Name)
	}

	args := make([]*mcpsdk.PromptArgument, 0, len(def.Arguments))
	for _, a := range def.Arguments {
		if a.Name == "" {
			return fmt.Errorf("dockyard/runtime/server: prompt %q has an empty-named argument", def.Name)
		}
		args = append(args, &mcpsdk.PromptArgument{
			Name:        a.Name,
			Title:       a.Title,
			Description: a.Description,
			Required:    a.Required,
		})
	}

	sdkPrompt := &mcpsdk.Prompt{
		Name:        def.Name,
		Title:       def.Title,
		Description: def.Description,
		Arguments:   args,
	}

	handler := func(ctx context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		if req != nil && req.Params != nil {
			ctx = WithRequestMeta(ctx, req.Params.Meta)
		}
		// Stamp the in-flight MCP session id so obs/v1 events emitted
		// from the prompt handler carry SessionID. There is no
		// prompt-side logging bridge yet, so the ServerSession itself is
		// not threaded onto ctx.
		ctx = withPromptRequestSession(ctx, req)
		// Open a span so handler-emitted obs/v1 events nest under it.
		span := obs.NewTraceFromContext(ctx)
		ctx = obs.WithSpan(ctx, span)
		// Emit the obs/v1 prompt.get lifecycle (RFC §11.2, P2). The end
		// event carries name + message count + serialized byte size — a
		// resource.read-shaped guardrail, not a tool.call-shaped capture
		// (D-152 reasoning: prompts have no typed input/output contract).
		endObs := s.rec.PromptGet(ctx, span, def.Name)

		var args map[string]string
		if req != nil && req.Params != nil {
			args = req.Params.Arguments
		}
		dockyardReq := PromptRequest{Name: def.Name, Arguments: args}

		var out PromptResult
		err := guardHandler(ctx, s.log, "prompt", def.Name, func() error {
			var herr error
			out, herr = fn(ctx, dockyardReq)
			return herr
		})

		messages := 0
		bytes := 0
		if err == nil {
			messages = len(out.Messages)
			if rendered, mErr := json.Marshal(renderMessages(out.Messages)); mErr == nil {
				bytes = len(rendered)
			}
		}
		endObs(messages, bytes, err)

		if err != nil {
			return nil, err
		}

		desc := out.Description
		if desc == "" {
			desc = def.Description
		}
		return &mcpsdk.GetPromptResult{
			Description: desc,
			Messages:    renderMessages(out.Messages),
		}, nil
	}

	addPromptSafe(s.mcp, sdkPrompt, handler)
	s.prompts = append(s.prompts, def.Name)
	return nil
}

// renderMessages lowers Dockyard PromptMessage values onto the SDK's
// PromptMessage type. The body is always a TextContent block; nil or
// empty inputs emit an empty slice rather than a nil result, so the
// rendered wire shape is always a JSON array (which the spec requires).
func renderMessages(in []PromptMessage) []*mcpsdk.PromptMessage {
	if len(in) == 0 {
		return []*mcpsdk.PromptMessage{}
	}
	out := make([]*mcpsdk.PromptMessage, 0, len(in))
	for _, m := range in {
		role := mcpsdk.Role(m.Role)
		out = append(out, &mcpsdk.PromptMessage{
			Role:    role,
			Content: &mcpsdk.TextContent{Text: m.Text},
		})
	}
	return out
}

// addPromptSafe registers a prompt on the underlying SDK server. The SDK's
// AddPrompt does not return an error, but Dockyard wraps the call so a
// future SDK that panics on a bad registration becomes a typed Dockyard
// error rather than a process crash (mirrors addToolSafe). The current
// SDK never panics here; the recover() is defensive.
func addPromptSafe(m *mcpsdk.Server, p *mcpsdk.Prompt, h mcpsdk.PromptHandler) {
	defer func() {
		// Defensive: today's SDK does not panic on AddPrompt. If a future
		// SDK does, the panic is swallowed rather than crashing the
		// server-assembly path; the caller observes the missing prompt
		// when prompts/list is empty. A typed-error pathway lands when
		// the SDK exposes one.
		_ = recover()
	}()
	m.AddPrompt(p, h)
}
