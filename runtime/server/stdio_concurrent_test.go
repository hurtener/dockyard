package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/tasks"
)

// TestServeStdioWithTasks_SharedStdoutSerialised proves the depth-audit R5/S1
// fix end to end: when serveStdioWithTasksOn drives the SDK-output relay and
// the Tasks mount pump CONCURRENTLY against ONE stdout, the two writers share
// the lockedWriter, so under `-race` neither produces a data race on stdout
// and every emitted frame is whole — never an interleave. Before the fix the
// two writers contended on real os.Stdout with only the kernel's per-Write
// atomicity (PIPE_BUF, 4096 on macOS/Linux) between them.
func TestServeStdioWithTasks_SharedStdoutSerialised(t *testing.T) {
	t.Parallel()

	// A real Tasks engine — its mount drives the pump path.
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}

	s, err := New(Info{Name: "stdio-race", Version: "0.1.0"}, &Options{
		Logger: slog.New(slog.DiscardHandler),
		Tasks:  engine,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// stdin is the seam we feed frames in through. stdout collects everything
	// the SDK-output relay and the mount pump emit. Both writers go through one
	// *lockedWriter (the S1 fix) — concurrent writes must not corrupt frames.
	stdinR, stdinW := io.Pipe()
	var stdout safeBuffer

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- s.serveStdioWithTasksOn(runCtx, stdinR, &stdout)
	}()

	// Hammer both writers concurrently. The mount-pump side replies to each
	// tasks/* request frame on stdin; the SDK-relay side replies to each MCP
	// request frame on stdin. We mix the two so the locked writer is contended.
	const perKind = 24
	var wg sync.WaitGroup
	wg.Add(2)

	// Tasks/list frames — the mount pump answers each (the engine has no
	// requestor context; result is an empty list which is fine for this test).
	go func() {
		defer wg.Done()
		for i := 0; i < perKind; i++ {
			frame, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1000 + i, "method": tasks.MethodList,
			})
			if err != nil {
				t.Errorf("marshal tasks/list: %v", err)
				return
			}
			if _, err := stdinW.Write(append(frame, '\n')); err != nil {
				return
			}
		}
	}()

	// MCP initialize frames — the SDK answers each on the in-process pipe;
	// the relay goroutine then copies the SDK's reply onto the locked stdout.
	// Initialize is a small, always-valid frame that yields a sizable response
	// (capability blocks) — enough to make the relay's io.Copy issue real
	// Write calls that contend with the mount pump.
	go func() {
		defer wg.Done()
		for i := 0; i < perKind; i++ {
			frame, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 2000 + i,
				"method": "initialize",
				"params": map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name": "race-probe", "version": "0.0.1",
					},
				},
			})
			if err != nil {
				t.Errorf("marshal initialize: %v", err)
				return
			}
			if _, err := stdinW.Write(append(frame, '\n')); err != nil {
				return
			}
		}
	}()
	wg.Wait()

	// Wait for the writers to drain through both paths, then close stdin so the
	// mount pump reaches EOF and serveStdioWithTasksOn returns cleanly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// Expect at minimum perKind tasks/list replies + perKind initialize
		// replies — they may arrive interleaved but each frame is on its own
		// line. A frame count short of the total means the pump or relay is
		// still flushing.
		if countLines(stdout.bytes()) >= perKind*2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	_ = stdinW.Close()
	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("serveStdioWithTasksOn did not return after stdin close")
	}

	// Validate every line on stdout is a well-formed JSON-RPC frame — proof
	// that neither writer corrupted the other.
	sc := bufio.NewScanner(bytes.NewReader(stdout.bytes()))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var taskReplies, initReplies int
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var env map[string]json.RawMessage
		if err := json.Unmarshal(line, &env); err != nil {
			t.Fatalf("non-JSON frame on stdout (interleaved write?): %q (err=%v)", line, err)
		}
		if env["jsonrpc"] == nil {
			t.Fatalf("frame missing jsonrpc field (interleaved write?): %s", line)
		}
		// Distinguish tasks-vs-init by the id range we used.
		var id float64
		if rid, ok := env["id"]; ok {
			_ = json.Unmarshal(rid, &id)
		}
		switch {
		case id >= 1000 && id < 1000+perKind:
			taskReplies++
		case id >= 2000 && id < 2000+perKind:
			initReplies++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan stdout: %v", err)
	}
	if taskReplies != perKind {
		t.Errorf("tasks/list replies on stdout = %d, want %d", taskReplies, perKind)
	}
	// The SDK may answer initialize once and then reject re-initializes; the
	// criterion is "all SDK replies were well-formed", not a fixed count.
	if initReplies == 0 {
		t.Errorf("no initialize replies seen on stdout — SDK relay did not run")
	}
}

// safeBuffer is a goroutine-safe bytes.Buffer for collecting stdout across the
// SDK-relay and mount-pump writers.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

func countLines(b []byte) int {
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
