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
