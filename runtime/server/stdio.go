package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file holds the stdio-transport half of the Tasks transport mount wiring
// (R2 — depth-audit remediation; RFC §8.2). The HTTP half lives in http.go.
//
// The go-sdk routes receiving methods through a fixed package-level dispatch
// table and rejects an unknown method — tasks/get, tasks/result, … — before
// any middleware runs. Over stdio there is no middleware seam at all: the SDK's
// StdioTransport reads os.Stdin and writes os.Stdout itself. So a stdio
// deployment that wants tasks/* runs the SDK server on a forwarded in-process
// pipe pair and runs the Tasks mount's frame pump on the real stdin/stdout —
// tasks/* frames are answered by the engine, every other frame flows through to
// the SDK server's pipe and the SDK's output flows straight back to stdout.

// serveStdioWithTasks serves the MCP protocol over the real process
// stdin/stdout with the Tasks transport mount in front (RFC §8.2). It is the
// stdio counterpart of HTTPHandler's mount wrapping and is reached from
// ServeStdio only when a Tasks engine is attached.
//
// The wiring: the SDK server runs on an mcp.IOTransport over an in-process
// pipe pair (sdkIn / sdkOut). The mount's ServeStdioFrames reads real stdin —
// a tasks/* request frame is answered by the engine; every other frame is
// written to the SDK's input pipe. A copy goroutine streams the SDK's output
// pipe straight to real stdout, so SDK-initiated frames (notifications,
// server→client requests) are never dropped.
func (s *Server) serveStdioWithTasks(ctx context.Context) error {
	// sdkIn:  Dockyard writes forwarded frames here  → the SDK reads them.
	// sdkOut: the SDK writes its responses here      → Dockyard copies to stdout.
	sdkInR, sdkInW := io.Pipe()
	sdkOutR, sdkOutW := io.Pipe()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The SDK server runs on the in-process pipe pair, not the real stdio.
	sdkErr := make(chan error, 1)
	go func() {
		sdkErr <- s.Run(runCtx, &mcpsdk.IOTransport{
			Reader: sdkInR,
			Writer: sdkOutW,
		})
	}()

	// Copy the SDK's output frames straight to real stdout. The SDK already
	// writes newline-delimited JSON, so this is a verbatim relay; tasks/*
	// responses are written separately by the mount's pump below, and the two
	// writers never interleave a single frame because each writes whole lines.
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		_, _ = io.Copy(os.Stdout, sdkOutR)
	}()

	// The mount's frame pump owns real stdin. A tasks/* frame is answered by
	// the engine and written to stdout by the pump; any other frame is handed
	// to forward, which writes it to the SDK's input pipe and returns nil — the
	// SDK's response arrives asynchronously on sdkOut and is relayed by the
	// copy goroutine above. Returning nil keeps the pump from emitting a
	// duplicate frame.
	forward := func(_ context.Context, frame []byte) ([]byte, error) {
		if _, err := sdkInW.Write(append(frame, '\n')); err != nil {
			return nil, fmt.Errorf("dockyard/runtime/server: forward stdio frame to SDK: %w", err)
		}
		return nil, nil
	}

	pumpErr := s.tasksMount.ServeStdioFrames(runCtx, os.Stdin, os.Stdout, forward)

	// Stdin reached EOF or ctx was cancelled — tear down the SDK server and
	// the copy goroutine cleanly so neither outlives the transport.
	cancel()
	_ = sdkInW.Close()
	_ = sdkOutW.Close()
	_ = sdkInR.Close()
	_ = sdkOutR.Close()
	<-copyDone
	serveErr := <-sdkErr

	if pumpErr != nil && !isExpectedShutdown(ctx, pumpErr) {
		return fmt.Errorf("dockyard/runtime/server: stdio tasks pump: %w", pumpErr)
	}
	if serveErr != nil && !isExpectedShutdown(ctx, serveErr) {
		return serveErr
	}
	return nil
}

// isExpectedShutdown reports whether err is the benign consequence of a clean
// teardown — a cancelled context or a closed pipe — rather than a real fault.
// Tearing the mount down closes the pipes the SDK server is reading/writing, so
// the SDK's Run returns a pipe-closed error that is expected, not a failure.
func isExpectedShutdown(ctx context.Context, err error) bool {
	if err == nil {
		return true
	}
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled)
}
