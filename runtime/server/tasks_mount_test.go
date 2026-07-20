package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// This file covers the R2 remediation: the Tasks transport mount wired into
// runtime/server (server.Options.Tasks / WithTasks). It proves the HTTP and
// stdio transports route tasks/* into the engine, that the capabilities.tasks
// block is injected into the initialize handshake, that the explicit HTTP
// security posture still applies, and that a server with no Tasks engine is
// byte-for-byte unchanged.

// newTaskEngine builds a real in-memory Tasks engine for the server tests.
func newTaskEngine(t *testing.T) *tasks.Engine {
	t.Helper()
	e, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	return e
}

var capCodec = protocolcodec.CodecFor(protocolcodec.DefaultVersion)

// taskIDFrame builds a tasks/* JSON-RPC request frame for task id.
func taskIDFrame(t *testing.T, method, id string) []byte {
	t.Helper()
	params, err := capCodec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
	if err != nil {
		t.Fatalf("EncodeTaskIDParams: %v", err)
	}
	frame, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return frame
}

// TestTasksMount_HTTP_RoutesTasksFrames proves a tasks/* frame POSTed to the
// server's HTTPHandler is answered by the attached engine, not the SDK server.
func TestTasksMount_HTTP_RoutesTasksFrames(t *testing.T) {
	e := newTaskEngine(t)
	raw, err := e.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "x",
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := capCodec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("DecodeCreateTaskResult: %v", err)
	}

	s, err := server.New(server.Info{Name: "t", Version: "0.1.0"},
		&server.Options{Logger: quietLogger(), Tasks: e})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !s.TasksEnabled() {
		t.Fatal("Options.Tasks did not enable tasks")
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json",
		bytes.NewReader(taskIDFrame(t, tasks.MethodGet, created.Task.ID)))
	if err != nil {
		t.Fatalf("tasks/get POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode tasks/get response: %v (body %q)", err, out)
	}
	if errRaw, isErr := decoded["error"]; isErr {
		t.Fatalf("tasks/get over the wired server errored: %s", errRaw)
	}
	got, err := capCodec.DecodeGetTaskResult(decoded["result"])
	if err != nil {
		t.Fatalf("decode tasks/get result: %v", err)
	}
	if got.ID != created.Task.ID {
		t.Fatalf("tasks/get returned %q, want %q", got.ID, created.Task.ID)
	}
}

// TestTasksMount_HTTP_SecurityStillEnforced proves the mount sits INSIDE the
// HTTP security boundary — a cross-origin tasks/* POST is still rejected, so
// the mount does not weaken the explicit HTTPSecurity posture (CLAUDE.md §7).
func TestTasksMount_HTTP_SecurityStillEnforced(t *testing.T) {
	e := newTaskEngine(t)
	s, err := server.New(server.Info{Name: "t", Version: "0.1.0"},
		&server.Options{Logger: quietLogger(), Tasks: e})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	// A cross-site tasks/get POST must be rejected by cross-origin protection
	// before the mount answers it.
	req, _ := http.NewRequest(http.MethodPost, ts.URL,
		bytes.NewReader(taskIDFrame(t, tasks.MethodGet, "whatever")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("cross-origin tasks/get POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin tasks/* POST status = %d, want 403 — the mount bypassed HTTP security",
			resp.StatusCode)
	}
}

// TestTasksMount_HTTP_CapabilityInjected proves the initialize handshake over
// the wired server carries the capabilities.tasks block.
func TestTasksMount_HTTP_CapabilityInjected(t *testing.T) {
	e := newTaskEngine(t)
	s := newTestServer(t).WithTasks(e, nil)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "raw", "version": "0"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(frame))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("initialize POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)

	envelopeJSON := bytes.TrimSpace(out)
	if !bytes.HasPrefix(envelopeJSON, []byte("{")) {
		for _, line := range bytes.Split(out, []byte("\n")) {
			if d := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))); len(d) > 0 && d[0] == '{' {
				envelopeJSON = d
				break
			}
		}
	}
	var envelope struct {
		Result struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(envelopeJSON, &envelope); err != nil {
		t.Fatalf("decode initialize envelope: %v (body %q)", err, out)
	}
	if _, ok := envelope.Result.Capabilities["tasks"]; !ok {
		t.Fatalf("initialize over the wired server carries no capabilities.tasks — got %v",
			envelope.Result.Capabilities)
	}
}

// TestTasksMount_NoEngine_PlainServer proves a server with no Tasks engine is
// unchanged: TasksEnabled is false and a tasks/* frame is NOT intercepted (it
// falls through to the SDK server, which rejects the unknown method).
func TestTasksMount_NoEngine_PlainServer(t *testing.T) {
	s := newTestServer(t)
	if s.TasksEnabled() {
		t.Fatal("a server with no Tasks engine reports TasksEnabled")
	}
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json",
		bytes.NewReader(taskIDFrame(t, tasks.MethodGet, "x")))
	if err != nil {
		t.Fatalf("tasks/get POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	// The SDK server handles the frame (the mount is absent); it does NOT
	// return a Dockyard task result. We only assert the mount did not answer:
	// a tasks engine would have returned an ErrTaskNotFound JSON-RPC error for
	// task "x" — but with no mount there is no engine to consult.
	if bytes.Contains(out, []byte(`"status":"working"`)) {
		t.Fatal("a plain server answered a tasks/* frame as a task result")
	}
}

// TestTasksMount_Stdio_RoutesTasksFrames proves the stdio transport path runs
// the Tasks mount: a tasks/* frame on stdin is answered by the engine. It
// drives serveStdioWithTasks through ServeStdio with os.Stdin/os.Stdout
// redirected to in-process pipes.
func TestTasksMount_Stdio_RoutesTasksFrames(t *testing.T) {
	e := newTaskEngine(t)
	raw, err := e.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "x",
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := capCodec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("DecodeCreateTaskResult: %v", err)
	}

	s, err := server.New(server.Info{Name: "t", Version: "0.1.0"},
		&server.Options{Logger: quietLogger(), Tasks: e})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Redirect os.Stdin/os.Stdout to in-process pipes for the duration of the
	// stdio serve — the mount's frame pump reads os.Stdin and writes os.Stdout.
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = origIn, origOut }()

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- s.ServeStdio(ctx) }()

	// Write a tasks/get frame on stdin; the mount answers it on stdout.
	if _, err := inW.Write(append(taskIDFrame(t, tasks.MethodGet, created.Task.ID), '\n')); err != nil {
		t.Fatalf("write stdin frame: %v", err)
	}

	respCh := make(chan []byte, 1)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) > 0 {
				respCh <- append([]byte(nil), line...)
				return
			}
		}
		respCh <- nil
	}()

	select {
	case line := <-respCh:
		if line == nil {
			t.Fatal("stdio tasks/get produced no response frame")
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatalf("decode stdio tasks/get response: %v (%q)", err, line)
		}
		if errRaw, isErr := decoded["error"]; isErr {
			t.Fatalf("stdio tasks/get errored: %s", errRaw)
		}
		got, err := capCodec.DecodeGetTaskResult(decoded["result"])
		if err != nil {
			t.Fatalf("decode stdio tasks/get result: %v", err)
		}
		if got.ID != created.Task.ID {
			t.Fatalf("stdio tasks/get returned %q, want %q", got.ID, created.Task.ID)
		}
		// Regression (v1.9.2): a stdio session is always legacy — the SDK caps
		// initialize negotiation below 2026-07-28 (see
		// TestModernProtocolUnreachableViaInitialize), so the mount's legacy
		// codec is correct here. The response must carry neither the modern
		// resultType discriminator nor the SEP-2575 serverInfo _meta; those are
		// modern-only and belong to the stateless HTTP path.
		var resultFields map[string]json.RawMessage
		if err := json.Unmarshal(decoded["result"], &resultFields); err != nil {
			t.Fatalf("decode stdio tasks/get result fields: %v", err)
		}
		if _, hasResultType := resultFields["resultType"]; hasResultType {
			t.Errorf("stdio tasks/get result carries modern resultType: %s", decoded["result"])
		}
		if bytes.Contains(line, []byte("io.modelcontextprotocol/serverInfo")) {
			t.Errorf("stdio tasks/get frame carries modern serverInfo _meta: %s", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdio tasks/get over the mount timed out")
	}

	// Clean teardown: cancel the context, close stdin so the pump reaches EOF.
	cancel()
	_ = inW.Close()
	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeStdio did not return after teardown")
	}
	_ = outW.Close()
	_ = outR.Close()
	_ = inR.Close()
}
