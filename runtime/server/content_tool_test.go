package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

func standardContentResult(_ context.Context, in echoIn) (server.ContentToolOutput[echoOut], error) {
	return server.ContentToolOutput[echoOut]{
		Text: "ready: " + in.Message,
		Content: []mcpsdk.Content{
			&mcpsdk.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"},
			&mcpsdk.AudioContent{Data: []byte{4, 5, 6}, MIMEType: "audio/mpeg"},
			&mcpsdk.ResourceLink{URI: "https://example.test/clip.mp4", Name: "clip.mp4", MIMEType: "video/mp4"},
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
				URI: "media://clip/0", MIMEType: "video/mp4", Blob: []byte{7, 8, 9},
			}},
		},
		Structured: echoOut{Echo: in.Message},
		Meta:       map[string]any{"view": "media"},
	}, nil
}

// TestAddContentToolWithSchemas_StandardBlocks proves the additive content
// path preserves deterministic text-first ordering, all allowed tools/call
// content variants, structuredContent, and _meta.
func TestAddContentToolWithSchemas_StandardBlocks(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddContentToolWithSchemas(s,
		server.ToolDef{Name: "standard-content"}, nil, nil, standardContentResult); err != nil {
		t.Fatalf("AddContentToolWithSchemas: %v", err)
	}
	res, err := connect(t, s).CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "standard-content", Arguments: echoIn{Message: "image"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 5 {
		t.Fatalf("content length = %d, want text plus four standard blocks", len(res.Content))
	}
	if got, ok := res.Content[0].(*mcpsdk.TextContent); !ok || got.Text != "ready: image" {
		t.Fatalf("content[0] = %#v, want text-first block", res.Content[0])
	}
	if _, ok := res.Content[1].(*mcpsdk.ImageContent); !ok {
		t.Fatalf("content[1] = %T, want ImageContent", res.Content[1])
	}
	if _, ok := res.Content[2].(*mcpsdk.AudioContent); !ok {
		t.Fatalf("content[2] = %T, want AudioContent", res.Content[2])
	}
	if _, ok := res.Content[3].(*mcpsdk.ResourceLink); !ok {
		t.Fatalf("content[3] = %T, want ResourceLink", res.Content[3])
	}
	if _, ok := res.Content[4].(*mcpsdk.EmbeddedResource); !ok {
		t.Fatalf("content[4] = %T, want EmbeddedResource", res.Content[4])
	}
	if res.Meta["view"] != "media" {
		t.Fatalf("result _meta = %#v, want view=media", res.Meta)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var out echoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Echo != "image" {
		t.Fatalf("structuredContent = %#v, want echo=image", out)
	}
}

// TestAddContentToolWithSchemas_RejectsNonToolResultContent proves the server
// validates the closed tools/call family before the SDK sees a result. The SDK
// exposes sampling-only types through the same Content interface, so accepting
// them would produce an invalid tools/call response.
func TestAddContentToolWithSchemas_RejectsNonToolResultContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content []mcpsdk.Content
		want    string
	}{
		{name: "nil interface", content: []mcpsdk.Content{nil}, want: "nil blocks"},
		{name: "typed nil", content: []mcpsdk.Content{(*mcpsdk.ImageContent)(nil)}, want: "nil blocks"},
		{name: "sampling tool use", content: []mcpsdk.Content{&mcpsdk.ToolUseContent{ID: "u1"}}, want: "not valid"},
		{name: "sampling tool result", content: []mcpsdk.Content{&mcpsdk.ToolResultContent{ToolUseID: "u1"}}, want: "not valid"},
		{name: "nil embedded resource", content: []mcpsdk.Content{&mcpsdk.EmbeddedResource{}}, want: "resource must not be nil"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			if err := server.AddContentToolWithSchemas(s,
				server.ToolDef{Name: "invalid-content"}, nil, nil,
				func(context.Context, echoIn) (server.ContentToolOutput[echoOut], error) {
					return server.ContentToolOutput[echoOut]{Content: tc.content}, nil
				}); err != nil {
				t.Fatalf("registration: %v", err)
			}
			res, err := connect(t, s).CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name: "invalid-content", Arguments: echoIn{Message: "bad"},
			})
			if err != nil {
				t.Fatalf("CallTool transport error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("invalid content should produce IsError=true: %+v", res)
			}
			if len(res.Content) != 1 {
				t.Fatalf("error content = %#v, want one diagnostic text block", res.Content)
			}
			text, ok := res.Content[0].(*mcpsdk.TextContent)
			if !ok || !strings.Contains(text.Text, tc.want) {
				t.Fatalf("error content = %#v, want diagnostic containing %q", res.Content[0], tc.want)
			}
		})
	}
}

// TestAddContentToolWithSchemas_RawHTTPWire proves the additive result path
// reaches the actual tools/call JSON wire with standard content blocks, without
// an application envelope or a duplicated structured payload.
func TestAddContentToolWithSchemas_RawHTTPWire(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddContentToolWithSchemas(s,
		server.ToolDef{Name: "wire-content"}, nil, nil, standardContentResult); err != nil {
		t.Fatalf("registration: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{
		ProtocolMode: server.Stateless20260728,
		Security:     server.DefaultHTTPSecurity(),
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	raw := modernRPC(t, ts, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wire-content","arguments":{"message":"raw"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	var envelope struct {
		Result struct {
			Content       []json.RawMessage `json:"content"`
			Structured    json.RawMessage   `json:"structuredContent"`
			IsError       bool              `json:"isError"`
			RequestState  string            `json:"requestState"`
			InputRequests json.RawMessage   `json:"inputRequests"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode raw tools/call response %s: %v", raw, err)
	}
	if envelope.Result.IsError {
		t.Fatalf("raw tools/call unexpectedly errored: %s", raw)
	}
	if len(envelope.Result.Content) != 5 {
		t.Fatalf("raw content length = %d, want five blocks: %s", len(envelope.Result.Content), raw)
	}
	var first struct{ Type, Text string }
	if err := json.Unmarshal(envelope.Result.Content[0], &first); err != nil || first.Type != "text" || first.Text != "ready: raw" {
		t.Fatalf("raw content[0] = %s, want text-first block", envelope.Result.Content[0])
	}
	var image struct{ Type, MIMEType, Data string }
	if err := json.Unmarshal(envelope.Result.Content[1], &image); err != nil || image.Type != "image" || image.MIMEType != "image/png" || image.Data == "" {
		t.Fatalf("raw content[1] = %s, want image block", envelope.Result.Content[1])
	}
	if len(envelope.Result.Structured) == 0 || !strings.Contains(string(envelope.Result.Structured), `"echo":"raw"`) {
		t.Fatalf("structuredContent = %s, want preserved typed output", envelope.Result.Structured)
	}
	if envelope.Result.RequestState != "" || len(envelope.Result.InputRequests) != 0 {
		t.Fatalf("content tool emitted continuation fields: %s", raw)
	}
}

// TestAddContentToolWithSchemas_RegistrationValidation keeps misuse loud and
// confirms the additive registration path does not alter the old tool list.
func TestAddContentToolWithSchemas_RegistrationValidation(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddContentToolWithSchemas[echoIn, echoOut](nil,
		server.ToolDef{Name: "nil-server"}, nil, nil, standardContentResult); err == nil {
		t.Fatal("nil server should error")
	}
	if err := server.AddContentToolWithSchemas(s,
		server.ToolDef{Name: ""}, nil, nil, standardContentResult); err == nil {
		t.Fatal("empty name should error")
	}
	if err := server.AddContentToolWithSchemas[echoIn, echoOut](s,
		server.ToolDef{Name: "nil-handler"}, nil, nil, nil); err == nil {
		t.Fatal("nil handler should error")
	}
	if err := server.AddContentToolWithSchemas(s,
		server.ToolDef{Name: "duplicate"}, nil, nil, standardContentResult); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := server.AddContentToolWithSchemas(s,
		server.ToolDef{Name: "duplicate"}, nil, nil, standardContentResult); err == nil {
		t.Fatal("duplicate registration should error")
	}
}
