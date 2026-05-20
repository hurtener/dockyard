package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// echoIn / echoOut are a trivial typed tool contract used across the tests.
type echoIn struct {
	Message string `json:"message"`
}

type echoOut struct {
	Echo string `json:"echo"`
}

func echoHandler(_ context.Context, in echoIn) (echoOut, error) {
	return echoOut{Echo: in.Message}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.New(server.Info{
		Name:    "test-app",
		Title:   "Test App",
		Version: "0.1.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		info    server.Info
		wantErr bool
	}{
		{"valid", server.Info{Name: "app", Version: "1.0.0"}, false},
		{"missing name", server.Info{Version: "1.0.0"}, true},
		{"missing version", server.Info{Name: "app"}, true},
		{"empty", server.Info{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := server.New(tc.info, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("New(%+v): want error, got nil", tc.info)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%+v): unexpected error: %v", tc.info, err)
			}
			if got := s.Info(); got != tc.info {
				t.Fatalf("Info() = %+v, want %+v", got, tc.info)
			}
		})
	}
}

func TestNew_NilOptionsUsesDefaultLogger(t *testing.T) {
	t.Parallel()
	s, err := server.New(server.Info{Name: "app", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("New with nil options: %v", err)
	}
	if s.MCP() == nil {
		t.Fatal("MCP() returned nil")
	}
}

func TestAddTool_Registration(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo", Description: "echo back"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	tools := s.Tools()
	if len(tools) != 1 || tools[0] != "echo" {
		t.Fatalf("Tools() = %v, want [echo]", tools)
	}

	// Tools() must return a defensive copy.
	tools[0] = "mutated"
	if s.Tools()[0] != "echo" {
		t.Fatal("Tools() leaked its internal slice")
	}
}

func TestAddTool_Errors(t *testing.T) {
	t.Parallel()
	t.Run("nil server", func(t *testing.T) {
		t.Parallel()
		if err := server.AddTool[echoIn, echoOut](nil, server.ToolDef{Name: "x"}, echoHandler); err == nil {
			t.Fatal("want error for nil server")
		}
	})
	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := server.AddTool(s, server.ToolDef{}, echoHandler); err == nil {
			t.Fatal("want error for empty name")
		}
	})
	t.Run("nil handler", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := server.AddTool[echoIn, echoOut](s, server.ToolDef{Name: "x"}, nil); err == nil {
			t.Fatal("want error for nil handler")
		}
	})
	t.Run("duplicate name", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
			t.Fatalf("first AddTool: %v", err)
		}
		if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err == nil {
			t.Fatal("want error for duplicate tool name")
		}
	})
	t.Run("non-object input rejected", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		// A string input has no object JSON Schema; the SDK rejects it. The
		// Dockyard wrapper must surface this as an error, never a panic.
		err := server.AddTool(s, server.ToolDef{Name: "bad"},
			func(_ context.Context, _ string) (echoOut, error) { return echoOut{}, nil })
		if err == nil {
			t.Fatal("want error for non-object input type")
		}
	})
}

func TestRun_NilTransport(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := s.Run(context.Background(), nil); err == nil {
		t.Fatal("Run(nil transport): want error")
	}
}

// connect serves the Dockyard server over an in-memory transport and returns a
// connected SDK client session — the contract-test backbone (brief 03 §2.3).
func connect(t *testing.T, s *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srvErr := make(chan error, 1)
	go func() { srvErr <- s.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return session
}

// TestServeAndCallTool is the acceptance test: a server registers one tool and
// serves it over a transport, and a client discovers and calls it end to end.
func TestServeAndCallTool(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo", Description: "echo back the message"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Fatalf("ListTools = %+v, want one tool named echo", list.Tools)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: echoIn{Message: "hello dockyard"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool returned IsError: %+v", res.Content)
	}

	// The typed output lands in StructuredContent (RFC §6.3).
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got echoOut
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if got.Echo != "hello dockyard" {
		t.Fatalf("echo = %q, want %q", got.Echo, "hello dockyard")
	}
}

// TestConcurrentReuse proves a single Server safely serves many sessions at
// once — the reusable-artifact concurrency requirement (AGENTS.md §5, §14).
func TestConcurrentReuse(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	const sessions = 8
	var wg sync.WaitGroup
	wg.Add(sessions)
	for i := range sessions {
		go func() {
			defer wg.Done()
			session := connect(t, s)
			res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      "echo",
				Arguments: echoIn{Message: "concurrent"},
			})
			if err != nil {
				t.Errorf("session %d CallTool: %v", i, err)
				return
			}
			if res.IsError {
				t.Errorf("session %d returned IsError", i)
			}
		}()
	}
	wg.Wait()
}
