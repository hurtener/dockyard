package server_test

import (
	"context"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestServeInMemory exercises the ServeInMemory entrypoint (RFC §5.2): the
// server runs over an in-memory transport and a client connected to the
// returned client-side transport drives a full protocol exchange.
func TestServeInMemory(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	if err := s.AddResource(server.ResourceDef{URI: "ui://im/page", Name: "page"},
		staticResource("in-memory")); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientT := s.ServeInMemory(ctx)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "im-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect over in-memory transport: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: echoIn{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool returned IsError: %+v", res.Content)
	}

	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://im/page"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "in-memory" {
		t.Fatalf("ReadResource = %+v, want the in-memory page body", read.Contents)
	}
}

// TestConcurrentResourceReads proves a single Server safely serves concurrent
// resource reads across many in-memory sessions under -race — the
// reusable-artifact concurrency requirement (AGENTS.md §5, §14).
func TestConcurrentResourceReads(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := s.AddResource(server.ResourceDef{URI: "ui://conc/page", Name: "page"},
		staticResource("concurrent")); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	const sessions = 8
	var wg sync.WaitGroup
	wg.Add(sessions)
	for i := range sessions {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clientT := s.ServeInMemory(ctx)
			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "conc", Version: "0.0.0"}, nil)
			session, err := client.Connect(ctx, clientT, nil)
			if err != nil {
				t.Errorf("session %d connect: %v", i, err)
				return
			}
			defer func() { _ = session.Close() }()
			read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://conc/page"})
			if err != nil {
				t.Errorf("session %d ReadResource: %v", i, err)
				return
			}
			if len(read.Contents) != 1 || read.Contents[0].Text != "concurrent" {
				t.Errorf("session %d resource body = %+v", i, read.Contents)
			}
		}()
	}
	wg.Wait()
}
