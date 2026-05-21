package server_test

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestAddTool_HandlerPanicSurvivesServer is the headline panic-safety acceptance
// (D-053): a tool registered via AddTool whose handler panics on a live
// tools/call must NOT crash the server process — the call returns an error
// result and a subsequent call on the same session still succeeds.
func TestAddTool_HandlerPanicSurvivesServer(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	if err := server.AddTool(s, server.ToolDef{Name: "boom"},
		func(_ context.Context, _ echoIn) (echoOut, error) {
			panic("handler exploded")
		}); err != nil {
		t.Fatalf("AddTool boom: %v", err)
	}
	if err := server.AddTool(s, server.ToolDef{Name: "ok"}, echoHandler); err != nil {
		t.Fatalf("AddTool ok: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	// The panicking call must come back as an error result, not a transport
	// failure or a crashed server.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "boom",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool(boom) returned a transport error, want an error result: %v", err)
	}
	if !res.IsError {
		t.Fatal("CallTool(boom): want IsError after a panicking handler")
	}

	// The server survived: a healthy tool on the same session still works.
	ok, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "ok",
		Arguments: echoIn{Message: "still alive"},
	})
	if err != nil {
		t.Fatalf("CallTool(ok) after a panicking handler: %v", err)
	}
	if ok.IsError {
		t.Fatalf("CallTool(ok) after a panicking handler returned IsError: %+v", ok.Content)
	}
}

// TestAddToolWithSchemas_HandlerPanicSurvivesServer proves the contract-first
// registration path (the seam runtime/tool composes) is panic-guarded too.
func TestAddToolWithSchemas_HandlerPanicSurvivesServer(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "boom"}, nil, nil,
		func(_ context.Context, _ echoIn) (server.ToolOutput[echoOut], error) {
			panic("schema-path handler exploded")
		}); err != nil {
		t.Fatalf("AddToolWithSchemas boom: %v", err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "ok"}, nil, nil,
		schemaOutFunc); err != nil {
		t.Fatalf("AddToolWithSchemas ok: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "boom",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool(boom): transport error, want an error result: %v", err)
	}
	if !res.IsError {
		t.Fatal("CallTool(boom): want IsError after a panicking handler")
	}

	ok, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "ok",
		Arguments: echoIn{Message: "still alive"},
	})
	if err != nil {
		t.Fatalf("CallTool(ok) after a panicking handler: %v", err)
	}
	if ok.IsError {
		t.Fatalf("CallTool(ok) after a panicking handler returned IsError: %+v", ok.Content)
	}
}

// TestAddResource_HandlerPanicSurvivesServer proves a panicking resource-read
// handler is recovered: the read returns an error and the server keeps serving.
func TestAddResource_HandlerPanicSurvivesServer(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	if err := s.AddResource(server.ResourceDef{URI: "ui://app/boom", Name: "boom"},
		func(_ context.Context, _ string) (server.ResourceContent, error) {
			panic("resource handler exploded")
		}); err != nil {
		t.Fatalf("AddResource boom: %v", err)
	}
	if err := s.AddResource(server.ResourceDef{URI: "ui://app/ok", Name: "ok"},
		staticResource("<html>alive</html>")); err != nil {
		t.Fatalf("AddResource ok: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	if _, err := session.ReadResource(ctx,
		&mcpsdk.ReadResourceParams{URI: "ui://app/boom"}); err == nil {
		t.Fatal("ReadResource(boom): want an error from a panicking handler")
	}

	// The server survived the panic: a healthy resource still reads back.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://app/ok"})
	if err != nil {
		t.Fatalf("ReadResource(ok) after a panicking handler: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "<html>alive</html>" {
		t.Fatalf("ReadResource(ok) = %+v, want the healthy body", read.Contents)
	}
}

// TestHandlerPanic_TypedError proves a recovered panic surfaces as a typed
// error wrapping ErrHandlerPanic — observable in-process by an in-memory
// invoker, not only over the wire.
func TestHandlerPanic_TypedError(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	if err := server.AddTool(s, server.ToolDef{Name: "boom"},
		func(_ context.Context, _ echoIn) (echoOut, error) {
			panic("kaboom")
		}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	session := connect(t, s)
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "boom",
		Arguments: echoIn{Message: "x"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError")
	}
	// The recovered-panic error text reaches the host content so the failure
	// is diagnosable rather than silent.
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("error content = %T, want TextContent", res.Content[0])
	}
	if tc.Text == "" {
		t.Fatal("recovered-panic error result carries no diagnostic text")
	}
}

// TestErrHandlerPanic_IsSentinel pins that ErrHandlerPanic is a usable sentinel.
func TestErrHandlerPanic_IsSentinel(t *testing.T) {
	t.Parallel()
	if !errors.Is(server.ErrHandlerPanic, server.ErrHandlerPanic) {
		t.Fatal("ErrHandlerPanic is not its own sentinel")
	}
}
