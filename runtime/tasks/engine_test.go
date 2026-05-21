package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// quietLogger returns a slog.Logger that discards output, so test runs stay
// readable.
func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// newEngine builds an Engine over a fresh in-memory store for a test.
func newEngine(t *testing.T, opts *Options) *Engine {
	t.Helper()
	if opts == nil {
		opts = &Options{}
	}
	if opts.Logger == nil {
		opts.Logger = quietLogger()
	}
	e, err := NewEngine(NewInMemoryStore(), opts)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// blockingRun returns a RunFunc that waits for release before producing out.
func blockingRun(release <-chan struct{}, out json.RawMessage, err error) RunFunc {
	return func(ctx context.Context) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return out, err
	}
}

// instantRun returns a RunFunc that produces out immediately.
func instantRun(out json.RawMessage, err error) RunFunc {
	return func(context.Context) (json.RawMessage, error) { return out, err }
}

func TestNewEngine_RejectsNilStore(t *testing.T) {
	t.Parallel()
	if _, err := NewEngine(nil, nil); err == nil {
		t.Fatal("want error for nil store")
	}
}

// TestCreateForToolCall_ReturnsCreateTaskResult covers the binding acceptance
// criterion: a task-augmented tools/call returns a CreateTaskResult.
func TestCreateForToolCall_ReturnsCreateTaskResult(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	raw, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{
		ToolName: "generate_report",
		Run:      instantRun(json.RawMessage(`{"isError":false}`), nil),
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("DecodeCreateTaskResult: %v", err)
	}
	if res.Task.ID == "" {
		t.Fatal("CreateTaskResult carries no task ID")
	}
	if res.Task.Status != protocolcodec.TaskWorking {
		t.Fatalf("task must begin in working, got %q", res.Task.Status)
	}
}

func TestCreateForToolCall_RejectsNilRun(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	_, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{ToolName: "x"})
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("want ErrInvalidParams for nil Run, got %v", err)
	}
}

// taskIDOf creates a task and returns its ID.
func taskIDOf(t *testing.T, e *Engine, run RunFunc) string {
	t.Helper()
	raw, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{ToolName: "x", Run: run})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return res.Task.ID
}

func dispatchTaskID(t *testing.T, e *Engine, method, id string) (json.RawMessage, error) {
	t.Helper()
	p, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).
		EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}
	return e.Dispatch(context.Background(), method, p)
}

// TestTasksGet_NonBlocking proves tasks/get returns the current state without
// blocking, even while the task is still working.
func TestTasksGet_NonBlocking(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	release := make(chan struct{})
	defer close(release)
	id := taskIDOf(t, e, blockingRun(release, nil, nil))

	raw, err := dispatchTaskID(t, e, MethodGet, id)
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	task, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.Status != protocolcodec.TaskWorking {
		t.Fatalf("tasks/get on a running task = %q, want working", task.Status)
	}
}

func TestTasksGet_UnknownTask(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	_, err := dispatchTaskID(t, e, MethodGet, "task_nonexistent")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
	if code := JSONRPCCode(err); code != CodeInvalidParams {
		t.Fatalf("JSONRPCCode = %d, want %d", code, CodeInvalidParams)
	}
}

// TestTasksResult_BlocksUntilTerminal proves tasks/result blocks while the task
// is working and returns the underlying result once terminal.
func TestTasksResult_BlocksUntilTerminal(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	release := make(chan struct{})
	want := json.RawMessage(`{"content":[{"type":"text","text":"done"}],"isError":false}`)
	id := taskIDOf(t, e, blockingRun(release, want, nil))

	done := make(chan json.RawMessage, 1)
	errc := make(chan error, 1)
	go func() {
		raw, err := dispatchTaskID(t, e, MethodResult, id)
		if err != nil {
			errc <- err
			return
		}
		done <- raw
	}()

	// tasks/result must still be blocked — the task has not been released.
	select {
	case <-done:
		t.Fatal("tasks/result returned before the task reached a terminal status")
	case err := <-errc:
		t.Fatalf("tasks/result errored early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release) // let the task complete
	select {
	case raw := <-done:
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("result not a JSON object: %v", err)
		}
		// The result carries the related-task _meta key.
		meta, _ := obj["_meta"].(map[string]any)
		rel, _ := meta["io.modelcontextprotocol/related-task"].(map[string]any)
		if rel["taskId"] != id {
			t.Fatalf("tasks/result missing related-task meta: %#v", obj)
		}
	case err := <-errc:
		t.Fatalf("tasks/result: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("tasks/result did not return after release")
	}
}

// TestTasksResult_FailedTaskReturnsError proves a failed task surfaces a
// JSON-RPC error from tasks/result.
func TestTasksResult_FailedTaskReturnsError(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	id := taskIDOf(t, e, instantRun(nil, errors.New("rate limit exceeded")))

	// Wait for the task to reach a terminal status, then fetch the result.
	waitTerminal(t, e, id)
	_, err := dispatchTaskID(t, e, MethodResult, id)
	if err == nil {
		t.Fatal("tasks/result on a failed task must return an error")
	}
	if code := JSONRPCCode(err); code != CodeInvalidParams {
		t.Fatalf("JSONRPCCode = %d, want %d", code, CodeInvalidParams)
	}
}

// TestTasksCancel_TransitionsToCancelled proves tasks/cancel moves a working
// task to cancelled and returns the cancelled task.
func TestTasksCancel_TransitionsToCancelled(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	release := make(chan struct{})
	defer close(release)
	id := taskIDOf(t, e, blockingRun(release, nil, nil))

	raw, err := dispatchTaskID(t, e, MethodCancel, id)
	if err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	task, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.Status != protocolcodec.TaskCancelled {
		t.Fatalf("tasks/cancel result status = %q, want cancelled", task.Status)
	}
}

// TestTasksCancel_AlreadyTerminalRejected proves cancelling a terminal task is
// rejected with -32602 — the binding lifecycle-enforcement criterion.
func TestTasksCancel_AlreadyTerminalRejected(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	id := taskIDOf(t, e, instantRun(json.RawMessage(`{}`), nil))
	waitTerminal(t, e, id)

	_, err := dispatchTaskID(t, e, MethodCancel, id)
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("want ErrAlreadyTerminal, got %v", err)
	}
	if code := JSONRPCCode(err); code != CodeInvalidParams {
		t.Fatalf("JSONRPCCode = %d, want %d", code, CodeInvalidParams)
	}
}

// TestTasksCancel_CooperativeCancellation proves tasks/cancel cancels the
// running handler's context.
func TestTasksCancel_CooperativeCancellation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	observed := make(chan struct{})
	id := taskIDOf(t, e, func(ctx context.Context) (json.RawMessage, error) {
		<-ctx.Done() // the handler cooperatively observes cancellation
		close(observed)
		return nil, ctx.Err()
	})
	if _, err := dispatchTaskID(t, e, MethodCancel, id); err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context was not cancelled by tasks/cancel")
	}
}

// TestTasksList_NotAdvertisedIsMethodNotFound proves tasks/list is not served
// when not advertised — the capability gates the operation.
func TestTasksList_NotAdvertisedIsMethodNotFound(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil) // AdvertiseList defaults off
	_, err := e.Dispatch(context.Background(), MethodList, nil)
	if !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("want ErrUnknownMethod, got %v", err)
	}
	if code := JSONRPCCode(err); code != CodeMethodNotFound {
		t.Fatalf("JSONRPCCode = %d, want %d", code, CodeMethodNotFound)
	}
}

// TestTasksList_PaginatesWhenAdvertised proves tasks/list returns a page of
// tasks and a cursor when advertised.
func TestTasksList_PaginatesWhenAdvertised(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{AdvertiseList: true, RequestorIdentifiable: true})
	for i := 0; i < 3; i++ {
		taskIDOf(t, e, instantRun(json.RawMessage(`{}`), nil))
	}
	raw, err := e.Dispatch(context.Background(), MethodList, nil)
	if err != nil {
		t.Fatalf("tasks/list: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeListTasksResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Tasks) != 3 {
		t.Fatalf("tasks/list returned %d tasks, want 3", len(res.Tasks))
	}
}

func TestDispatch_UnknownMethod(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	_, err := e.Dispatch(context.Background(), "tasks/bogus", nil)
	if !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("want ErrUnknownMethod, got %v", err)
	}
}

func TestDispatch_MalformedParams(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	_, err := e.Dispatch(context.Background(), MethodGet, json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("want error for malformed params")
	}
	if code := JSONRPCCode(err); code != CodeInvalidParams {
		t.Fatalf("JSONRPCCode = %d, want %d", code, CodeInvalidParams)
	}
}

// TestEngineCapability proves the advertised capability reflects the engine's
// configuration — capability-driven, never a hardcoded matrix.
func TestEngineCapability(t *testing.T) {
	t.Parallel()
	off := newEngine(t, nil).Capability()
	if off.List {
		t.Error("tasks/list must not be advertised by default")
	}
	if !off.Cancel || !off.ToolsCall {
		t.Error("cancel + tools.call must always be advertised")
	}
	on := newEngine(t, &Options{AdvertiseList: true, RequestorIdentifiable: true}).Capability()
	if !on.List {
		t.Error("AdvertiseList did not enable the list capability")
	}
}

// TestHandlerPanicBecomesFailedTask proves a panicking handler does not crash
// the engine — the task fails, the boundary holds (AGENTS.md §13).
func TestHandlerPanicBecomesFailedTask(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	id := taskIDOf(t, e, func(context.Context) (json.RawMessage, error) {
		panic("handler boom")
	})
	rec := waitTerminal(t, e, id)
	if rec.Status != protocolcodec.TaskFailed {
		t.Fatalf("panicking handler should fail the task, got %q", rec.Status)
	}
}

// waitTerminal polls a task until it reaches a terminal status.
func waitTerminal(t *testing.T, e *Engine, id string) TaskRecord {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		rec, err := e.store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("store.Get: %v", err)
		}
		if rec.Status.IsTerminal() {
			return rec
		}
		select {
		case <-deadline:
			t.Fatalf("task %q did not reach a terminal status", id)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
