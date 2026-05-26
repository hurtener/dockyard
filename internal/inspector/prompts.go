package inspector

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// promptTimeout bounds one operator-initiated prompts/list or prompts/get
// — a connect, the RPC, and a result read. Same shape as invokeTimeout
// (D-131); a slow server surfaces as a typed error rather than stalling
// the panel.
const promptTimeout = 30 * time.Second

// PromptArgumentInfo describes one argument an MCP Prompt accepts, as
// surfaced to the inspector frontend. The shape mirrors the runtime's
// PromptArgument verbatim so the inspector form generates the same
// fields a host would surface to a user. Strings only — MCP prompt
// arguments are flat string-keyed maps (D-152).
type PromptArgumentInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptInfo describes one registered MCP Prompt the inspector lists
// on `GET /api/prompts`. It is the inspector's own type — no raw SDK
// struct leaks (P3, mirroring InvokeResponse and AppPreview).
type PromptInfo struct {
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description,omitempty"`
	Arguments   []PromptArgumentInfo `json:"arguments,omitempty"`
}

// PromptGetRequest is the operator's typed request to
// `POST /api/prompts/get`: the prompt name and its flat string-keyed
// argument map. The map shape comes straight from the MCP wire format
// (D-152 — prompt arguments are flat strings).
type PromptGetRequest struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptGetMessage is one rendered prompt message — a role plus a text
// body. Mirrors runtime/server.PromptMessage's text-only shape (D-151).
type PromptGetMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// PromptGetResponse is the JSON the inspector returns from a successful
// `POST /api/prompts/get`. Messages is the rendered message list; an
// empty Messages slice serialises as `[]` so the frontend renders
// "rendered no messages" rather than null.
type PromptGetResponse struct {
	// Description is the optional rendered description override; falls
	// back to the registered PromptDef.Description on the server.
	Description string `json:"description,omitempty"`
	// Messages is the rendered messages, in order.
	Messages []PromptGetMessage `json:"messages"`
	// Error, when non-empty, carries a server-side prompts/get error —
	// a successful RPC where the server reported a typed error. The
	// frontend renders the panel's error region without conflating it
	// with a transport-level failure (the 200-with-error pattern
	// D-131 set for tools/invoke; D-163 extends it to prompts/get).
	Error string `json:"error,omitempty"`
}

// PromptSource produces the attached server's registered prompts. The
// inspector calls it per `GET /api/prompts` request. It is the
// read-only listing surface (D-103 read-only pattern, extended).
type PromptSource func(ctx context.Context) ([]PromptInfo, error)

// PromptInvoker performs one operator-initiated prompts/get against the
// attached MCP server and returns the rendered messages. The inspector
// calls it per `POST /api/prompts/get` request. It is the lone
// client-shaped mutating-shaped surface for prompts (D-163 extends
// D-131 to a third operator-initiated read).
//
// "Mutating-shaped" rather than mutating: a prompts/get is a read in
// the MCP semantic sense (the host pulls a rendered template — no
// side-effect on the server), but the request body is operator
// authored and the inspector treats it under the same operator-
// initiated framing as tools/call so the audit shape is uniform.
type PromptInvoker func(ctx context.Context, req PromptGetRequest) (*PromptGetResponse, error)

// PromptsFromServer adapts a running MCP server, named by its base URL,
// into a [PromptSource] + [PromptInvoker] pair. It is the v1.1 wave A
// (D-163) extension of the inspector's operator-initiated client-
// shaped surfaces — the pattern D-103 set for resources/read, D-131
// set for tools/call, D-134 set for tasks/result.
//
// Each call opens a fresh, short-lived MCP client session, makes one
// RPC, and closes — no long-lived production client. The inspector
// stays within P4: the lone client-shaped component, dev-mode-gated,
// localhost-bound (the listener's requireLoopback gate enforces it).
//
// A nil baseURL yields a pair that returns an error on call —
// without an attached server there is nothing to list or invoke, and
// the frontend's PromptsPanel surfaces the error in its panel state.
func PromptsFromServer(baseURL string) (PromptSource, PromptInvoker) {
	source := func(ctx context.Context) ([]PromptInfo, error) {
		if baseURL == "" {
			return nil, errors.New(
				"dockyard/internal/inspector: inspector is detached — " +
					"no server URL to list prompts against")
		}
		return listAttachedPrompts(ctx, baseURL)
	}
	invoker := func(ctx context.Context, req PromptGetRequest) (*PromptGetResponse, error) {
		if baseURL == "" {
			return nil, errors.New(
				"dockyard/internal/inspector: inspector is detached — " +
					"no server URL to invoke prompts against")
		}
		return getAttachedPrompt(ctx, baseURL, req)
	}
	return source, invoker
}

// listAttachedPrompts connects a short-lived MCP client to baseURL,
// calls prompts/list (walks the first page only — V1 pagination
// caveat documented in the panel), and closes. The result is the
// inspector's typed PromptInfo slice — no raw SDK type leaks through
// it (P3).
func listAttachedPrompts(ctx context.Context, baseURL string) ([]PromptInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()

	session, err := dialAttachedPrompt(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	result, err := session.ListPrompts(ctx, &mcpsdk.ListPromptsParams{})
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: prompts/list: %w", err)
	}
	if result == nil {
		return []PromptInfo{}, nil
	}
	out := make([]PromptInfo, 0, len(result.Prompts))
	for _, p := range result.Prompts {
		if p == nil {
			continue
		}
		info := PromptInfo{
			Name:        p.Name,
			Title:       p.Title,
			Description: p.Description,
		}
		if len(p.Arguments) > 0 {
			info.Arguments = make([]PromptArgumentInfo, 0, len(p.Arguments))
			for _, a := range p.Arguments {
				if a == nil {
					continue
				}
				info.Arguments = append(info.Arguments, PromptArgumentInfo{
					Name:        a.Name,
					Title:       a.Title,
					Description: a.Description,
					Required:    a.Required,
				})
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// getAttachedPrompt connects a short-lived MCP client to baseURL,
// calls prompts/get with the operator-supplied arguments, and closes.
// A successful RPC that returned a typed error is converted into a
// PromptGetResponse with Error filled — HTTP stays 200, mirroring
// D-131's IsError-as-200 pattern.
func getAttachedPrompt(ctx context.Context, baseURL string, req PromptGetRequest) (*PromptGetResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()

	session, err := dialAttachedPrompt(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	params := &mcpsdk.GetPromptParams{Name: req.Name}
	if len(req.Arguments) > 0 {
		params.Arguments = req.Arguments
	}
	result, err := session.GetPrompt(ctx, params)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: prompts/get %q: %w", req.Name, err)
	}
	if result == nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: prompts/get %q returned no result", req.Name)
	}

	resp := &PromptGetResponse{
		Description: result.Description,
		Messages:    make([]PromptGetMessage, 0, len(result.Messages)),
	}
	for _, m := range result.Messages {
		if m == nil {
			continue
		}
		resp.Messages = append(resp.Messages, PromptGetMessage{
			Role: string(m.Role),
			Text: extractPromptMessageText(m.Content),
		})
	}
	return resp, nil
}

// dialAttachedPrompt opens a short-lived MCP client session against
// baseURL. Shared by list + get so the dial shape is one place. The
// returned session is the caller's responsibility to Close — same
// shape as invoke.go's invokeAttachedTool.
func dialAttachedPrompt(ctx context.Context, baseURL string) (*mcpsdk.ClientSession, error) {
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-inspector", Version: "0.1.0"},
		nil,
	)
	session, err := client.Connect(ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: baseURL}, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"dockyard/internal/inspector: connect %q: %w", baseURL, err)
	}
	return session, nil
}

// extractPromptMessageText pulls a plain text body from a prompt
// message Content variant. The runtime exposes the text-only message
// shape (D-151); a server that returns a richer variant is rendered
// as a description string ("<resource …>", "<image …>") rather than
// a hard failure — the inspector is a debugging surface, not a host.
func extractPromptMessageText(content mcpsdk.Content) string {
	if content == nil {
		return ""
	}
	switch c := content.(type) {
	case *mcpsdk.TextContent:
		return c.Text
	case *mcpsdk.ImageContent:
		return "<image " + c.MIMEType + ">"
	case *mcpsdk.AudioContent:
		return "<audio " + c.MIMEType + ">"
	case *mcpsdk.EmbeddedResource:
		if c.Resource != nil {
			return "<resource " + c.Resource.URI + ">"
		}
		return "<resource>"
	default:
		return fmt.Sprintf("<%T>", content)
	}
}
