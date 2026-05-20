package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestServeStdio exercises the stdio entrypoint (RFC §5.2). It connects to the
// process stdin/stdout, so the test only asserts that the call returns once
// the context is cancelled rather than driving a protocol exchange — the
// end-to-end protocol path is covered by TestServeAndCallTool over the
// in-memory transport.
func TestServeStdio(t *testing.T) {
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.ServeStdio(ctx) }()

	cancel()
	select {
	case <-done:
		// Returned (with or without a transport error) — the goal is that
		// ServeStdio honours context cancellation and does not hang.
	case <-time.After(2 * time.Second):
		t.Fatal("ServeStdio did not return after context cancellation")
	}
}
