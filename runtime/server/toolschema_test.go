package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// schemaOutFunc is a trivial ToolOutputFunc for the AddToolWithSchemas tests.
func schemaOutFunc(_ context.Context, in echoIn) (server.ToolOutput[echoOut], error) {
	return server.ToolOutput[echoOut]{
		Text:       "echoed: " + in.Message,
		Structured: echoOut{Echo: in.Message},
		Meta:       map[string]any{"k": "v"},
	}, nil
}

func TestAddToolWithSchemas_Validation(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	if err := server.AddToolWithSchemas[echoIn, echoOut](nil,
		server.ToolDef{Name: "x"}, nil, nil, schemaOutFunc); err == nil {
		t.Error("nil server should error")
	}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: ""}, nil, nil, schemaOutFunc); err == nil {
		t.Error("empty name should error")
	}
	if err := server.AddToolWithSchemas[echoIn, echoOut](s,
		server.ToolDef{Name: "nilfn"}, nil, nil, nil); err == nil {
		t.Error("nil handler should error")
	}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: "dup"}, nil, nil, schemaOutFunc); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: "dup"}, nil, nil, schemaOutFunc); err == nil {
		t.Error("double register should error")
	}
}

// TestAddToolWithSchemas_ExplicitSchemaAndRouting registers a tool with an
// explicit, caller-supplied schema and asserts the registered tool carries that
// schema and that ToolOutput.Text/Structured route to content/structuredContent.
func TestAddToolWithSchemas_ExplicitSchemaAndRouting(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	inSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"message": {Type: "string", Description: "the message to echo"},
		},
		Required: []string{"message"},
	}
	outSchema := &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{"echo": {Type: "string"}},
		Required:   []string{"echo"},
	}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: "echo", Description: "echo"},
		inSchema, outSchema, schemaOutFunc); err != nil {
		t.Fatalf("AddToolWithSchemas: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(list.Tools))
	}
	gotIn, _ := json.Marshal(list.Tools[0].InputSchema)
	if want := `"the message to echo"`; !strings.Contains(string(gotIn), want) {
		t.Errorf("registered input schema %s missing the explicit description", gotIn)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok || tc.Text != "echoed: hi" {
		t.Errorf("content = %+v, want the ToolOutput.Text", res.Content[0])
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out echoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	if out.Echo != "hi" {
		t.Errorf("structuredContent echo = %q, want hi", out.Echo)
	}
	if res.Meta["k"] != "v" {
		t.Errorf("result _meta = %+v, want k=v", res.Meta)
	}
}

// TestAddToolWithSchemas_NoEmptyTextBlock proves the D-043 fix: a handler that
// returns no model-facing text (ToolOutput.Text == "") yields a result with
// zero content blocks — no empty TextContent — while the typed output still
// lands in structuredContent (RFC §6.3).
func TestAddToolWithSchemas_NoEmptyTextBlock(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	silent := func(_ context.Context, in echoIn) (server.ToolOutput[echoOut], error) {
		return server.ToolOutput[echoOut]{
			// No Text — a UI-only tool result.
			Structured: echoOut{Echo: in.Message},
		}, nil
	}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: "silent"}, nil, nil, silent); err != nil {
		t.Fatalf("AddToolWithSchemas: %v", err)
	}

	session := connect(t, s)
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "silent",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 0 {
		t.Fatalf("content = %d blocks, want 0 (no empty TextContent block — D-043)", len(res.Content))
	}
	// The typed output still routes to structuredContent — the empty-text fix
	// must not suppress the structured payload.
	raw, _ := json.Marshal(res.StructuredContent)
	var out echoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Echo != "hi" {
		t.Errorf("structuredContent echo = %q, want hi", out.Echo)
	}
}

// TestAddToolWithSchemas_StandardContent preserves the official MCP content
// variants alongside model-facing text and structured output. The server must
// carry these blocks as content[] without inventing an extension envelope.
func TestAddToolWithSchemas_StandardContent(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	image := &mcpsdk.ImageContent{Data: []byte{0x89, 0x50, 0x4e, 0x47}, MIMEType: "image/png"}
	audio := &mcpsdk.AudioContent{Data: []byte{0x49, 0x44, 0x33}, MIMEType: "audio/mpeg"}
	resource := &mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
		URI:      "media://video/1",
		MIMEType: "video/mp4",
		Blob:     []byte{0x00, 0x00, 0x00, 0x18},
		Meta:     mcpsdk.Meta{"name": "clip.mp4"},
	}}
	if err := server.AddToolWithSchemas(s,
		server.ToolDef{Name: "standard-content"}, nil, nil,
		func(context.Context, echoIn) (server.ToolOutput[echoOut], error) {
			return server.ToolOutput[echoOut]{
				Text:       "ready",
				Content:    []mcpsdk.Content{image, audio, resource},
				Structured: echoOut{Echo: "structured"},
			}, nil
		}); err != nil {
		t.Fatalf("AddToolWithSchemas: %v", err)
	}

	res, err := connect(t, s).CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "standard-content",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 4 {
		t.Fatalf("content length = %d, want text plus three standard blocks", len(res.Content))
	}
	if got, ok := res.Content[0].(*mcpsdk.TextContent); !ok || got.Text != "ready" {
		t.Fatalf("content[0] = %#v, want model-facing text", res.Content[0])
	}
	gotImage, ok := res.Content[1].(*mcpsdk.ImageContent)
	if !ok || gotImage.MIMEType != "image/png" || string(gotImage.Data) != string(image.Data) {
		t.Fatalf("content[1] = %#v, want image/png bytes", res.Content[1])
	}
	gotAudio, ok := res.Content[2].(*mcpsdk.AudioContent)
	if !ok || gotAudio.MIMEType != "audio/mpeg" || string(gotAudio.Data) != string(audio.Data) {
		t.Fatalf("content[2] = %#v, want audio/mpeg bytes", res.Content[2])
	}
	gotResource, ok := res.Content[3].(*mcpsdk.EmbeddedResource)
	if !ok || gotResource.Resource == nil || gotResource.Resource.URI != resource.Resource.URI ||
		gotResource.Resource.MIMEType != resource.Resource.MIMEType ||
		string(gotResource.Resource.Blob) != string(resource.Resource.Blob) ||
		gotResource.Resource.Meta["name"] != "clip.mp4" {
		t.Fatalf("content[3] = %#v, want embedded video resource", res.Content[3])
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out echoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	if out.Echo != "structured" {
		t.Fatalf("structuredContent = %#v, want preserved typed output", out)
	}
}
