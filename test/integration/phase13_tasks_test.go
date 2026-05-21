// This file is the Phase 13 cross-subsystem integration test (AGENTS.md §17).
// Phase 13's Deps name shipped phases — Phase 07's runtime/server core and
// Phase 02's internal/protocolcodec — and Phase 13 opens the server-side MCP
// Tasks seam Phase 14 builds on (RFC §8.1–§8.3). The test drives the surface
// end to end with real drivers: a real runtime/tasks.Engine over the real
// in-memory TaskStore and the real internal/protocolcodec codec — no mocks at
// the boundary. It exercises the full task lifecycle (a task-augmented
// tools/call → CreateTaskResult → tasks/get poll → tasks/result) and two
// failure modes: an illegal lifecycle transition rejected by the store, and a
// tasks/cancel of an already-terminal task rejected with the spec's -32602.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// codec is the real protocolcodec codec — the seam under test, not a mock.
var codec = protocolcodec.CodecFor(protocolcodec.DefaultVersion)

// newTaskEngine builds a real Engine over a real in-memory store.
func newTaskEngine(t *testing.T, advertiseList bool) *tasks.Engine {
	t.Helper()
	e, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		AdvertiseList: advertiseList,
		// Phase 14: tasks/list is served only when the engine can also identify
		// requestors (RFC §8.5). The Phase 13 listing tests want it served.
		RequestorIdentifiable: advertiseList,
		PollInterval:          10, // fast cadence keeps the test snappy
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func taskIDParams(t *testing.T, id string) json.RawMessage {
	t.Helper()
	p, err := codec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
	if err != nil {
		t.Fatalf("EncodeTaskIDParams: %v", err)
	}
	return p
}

// TestPhase13_TaskLifecycleEndToEnd drives a task-augmented tools/call through
// the engine: create → CreateTaskResult, poll → tasks/get, terminal result →
// tasks/result. The CallToolResult the synchronous path would have produced is
// returned verbatim by tasks/result.
func TestPhase13_TaskLifecycleEndToEnd(t *testing.T) {
	t.Parallel()
	e := newTaskEngine(t, false)
	ctx := context.Background()

	// The underlying tool work — a real CallToolResult-shaped payload.
	release := make(chan struct{})
	want := json.RawMessage(`{"content":[{"type":"text","text":"report ready"}],"isError":false}`)
	run := func(ctx context.Context) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return want, nil
	}

	// 1. A task-augmented tools/call returns a CreateTaskResult, status working.
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "generate_report",
		TaskMeta: protocolcodec.TaskMeta{TTL: ptrInt64(60000)},
		Run:      run,
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := codec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("DecodeCreateTaskResult: %v", err)
	}
	if created.Task.Status != protocolcodec.TaskWorking {
		t.Fatalf("created task status = %q, want working", created.Task.Status)
	}
	id := created.Task.ID

	// 2. tasks/get is non-blocking — it returns `working` while the task runs.
	getRaw, err := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	polled, err := codec.DecodeGetTaskResult(getRaw)
	if err != nil {
		t.Fatalf("DecodeGetTaskResult: %v", err)
	}
	if polled.Status != protocolcodec.TaskWorking {
		t.Fatalf("tasks/get on a running task = %q, want working", polled.Status)
	}

	// 3. tasks/result blocks until terminal; release the work, then collect it.
	resultCh := make(chan json.RawMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := e.Dispatch(ctx, tasks.MethodResult, taskIDParams(t, id))
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- r
	}()
	close(release)

	select {
	case got := <-resultCh:
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("tasks/result not a JSON object: %v", err)
		}
		if obj["isError"] != false {
			t.Fatalf("tasks/result lost the underlying CallToolResult: %#v", obj)
		}
		// The related-task _meta key is stamped on the result.
		meta, _ := obj["_meta"].(map[string]any)
		rel, _ := meta["io.modelcontextprotocol/related-task"].(map[string]any)
		if rel["taskId"] != id {
			t.Fatalf("tasks/result missing related-task meta: %#v", obj)
		}
	case err := <-errCh:
		t.Fatalf("tasks/result: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("tasks/result did not return")
	}

	// 4. After completion tasks/get reports the terminal status.
	finalRaw, err := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("final tasks/get: %v", err)
	}
	final, err := codec.DecodeGetTaskResult(finalRaw)
	if err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if final.Status != protocolcodec.TaskCompleted {
		t.Fatalf("final status = %q, want completed", final.Status)
	}
}

// TestPhase13_TasksListPaginatesOverRealEngine proves tasks/list, once
// advertised, pages over the real engine + store seam.
func TestPhase13_TasksListPaginatesOverRealEngine(t *testing.T) {
	t.Parallel()
	e := newTaskEngine(t, true)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
			ToolName: "t",
			Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
		}); err != nil {
			t.Fatalf("CreateForToolCall: %v", err)
		}
	}
	listRaw, err := e.Dispatch(ctx, tasks.MethodList, nil)
	if err != nil {
		t.Fatalf("tasks/list: %v", err)
	}
	list, err := codec.DecodeListTasksResult(listRaw)
	if err != nil {
		t.Fatalf("DecodeListTasksResult: %v", err)
	}
	if len(list.Tasks) != 4 {
		t.Fatalf("tasks/list returned %d tasks, want 4", len(list.Tasks))
	}
}

// TestPhase13_FailureMode_IllegalTransition is the first mandated failure mode:
// the lifecycle is enforced — an illegal transition through the real store is a
// typed error, never a panic across the boundary.
func TestPhase13_FailureMode_IllegalTransition(t *testing.T) {
	t.Parallel()
	store := tasks.NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.Create(ctx, tasks.TaskRecord{
		ID:        "t-illegal",
		Status:    protocolcodec.TaskWorking,
		CreatedAt: now,
		UpdatedAt: now,
		Method:    "tools/call",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Drive working → completed (legal, terminal).
	if _, err := store.Transition(ctx, "t-illegal", protocolcodec.TaskCompleted, "done"); err != nil {
		t.Fatalf("legal transition errored: %v", err)
	}
	// completed → working is illegal — a terminal status is immutable.
	_, err := store.Transition(ctx, "t-illegal", protocolcodec.TaskWorking, "")
	if !errors.Is(err, tasks.ErrIllegalTransition) {
		t.Fatalf("want ErrIllegalTransition for completed→working, got %v", err)
	}
}

// TestPhase13_FailureMode_CancelTerminal is the second mandated failure mode:
// tasks/cancel of an already-terminal task is rejected with the spec's -32602.
func TestPhase13_FailureMode_CancelTerminal(t *testing.T) {
	t.Parallel()
	e := newTaskEngine(t, false)
	ctx := context.Background()
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "t",
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := codec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Task.ID

	// Wait for the instant task to reach a terminal status via tasks/result.
	if _, err := e.Dispatch(ctx, tasks.MethodResult, taskIDParams(t, id)); err != nil {
		t.Fatalf("tasks/result: %v", err)
	}
	// Cancelling a completed task must be rejected with -32602 (Invalid params).
	_, err = e.Dispatch(ctx, tasks.MethodCancel, taskIDParams(t, id))
	if !errors.Is(err, tasks.ErrAlreadyTerminal) {
		t.Fatalf("want ErrAlreadyTerminal, got %v", err)
	}
	if code := tasks.JSONRPCCode(err); code != tasks.CodeInvalidParams {
		t.Fatalf("JSON-RPC code = %d, want %d", code, tasks.CodeInvalidParams)
	}
}

func ptrInt64(v int64) *int64 { return &v }
