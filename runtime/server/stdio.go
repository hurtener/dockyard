package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

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

// lockedWriter is a sync.Mutex-backed io.Writer adapter that serialises Write
// calls to an underlying writer. It is the seam serveStdioWithTasks uses to
// share one mutex between the SDK-output relay goroutine and the Tasks mount
// pump (R5 — depth-audit remediation; D-119): a single os.Stdout write is
// atomic only up to PIPE_BUF (4096 on macOS/Linux), and io.Copy uses a 32 KB
// buffer, so a large SDK frame can be split by the kernel and intersperse with
// a mount-written frame mid-emission unless the two writers share a lock. Both
// writers go through one *lockedWriter, so every Write is serialised end-to-end
// and a frame never interleaves with another.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// newLockedWriter wraps w so concurrent Write calls are serialised.
func newLockedWriter(w io.Writer) *lockedWriter { return &lockedWriter{w: w} }

// Write satisfies io.Writer; it serialises against every other Write on the
// same *lockedWriter, so a frame larger than the kernel pipe atomicity bound
// still emerges intact.
func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

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
//
// All writes to real os.Stdout go through ONE *lockedWriter shared between
// the SDK-output relay and the mount pump (D-119): a Write to a pipe is atomic
// only up to PIPE_BUF (4096 on macOS/Linux) and io.Copy uses a 32 KB buffer,
// so a large SDK frame can be split by the kernel and intersperse with a
// mount-written frame. The shared lock makes the two writers mutually
// exclusive, so a frame on the wire is always whole — the property the stdio
// JSON-RPC pipe contract requires.
func (s *Server) serveStdioWithTasks(ctx context.Context) error {
	return s.serveStdioWithTasksOn(ctx, os.Stdin, os.Stdout)
}

// serveStdioWithTasksOn is serveStdioWithTasks parameterised over its stdin
// and stdout, so a -race concurrency test can drive both writers on an
// in-memory sink (no real OS pipe required) and assert frames never
// interleave.
func (s *Server) serveStdioWithTasksOn(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
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

	// The single lock on stdout shared by BOTH writers — the SDK-output relay
	// and the mount pump. See the file header: PIPE_BUF + the io.Copy buffer
	// would otherwise let a large SDK frame interleave with a mount-written
	// frame mid-emission (D-119).
	out := newLockedWriter(stdout)

	// Copy the SDK's output frames to real stdout through the shared lock.
	// io.Copy issues Write calls of up to its 32 KB buffer; each one is
	// serialised against any concurrent mount-pump write so the two writers
	// never interleave a single frame.
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		_, _ = io.Copy(out, sdkOutR)
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

	pumpErr := s.tasksMount.ServeStdioFrames(runCtx, stdin, out, forward)

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
