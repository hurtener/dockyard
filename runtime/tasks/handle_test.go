package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
)

// captureEmitter is a concurrency-safe obs.Emitter that retains every event for
// assertions — the tasks test analogue of obs's own test collector.
type captureEmitter struct {
	mu     sync.Mutex
	events []obs.Event
}

func (c *captureEmitter) Emit(_ context.Context, e obs.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureEmitter) byKindPhase(kind obs.EventKind, phase obs.Phase) []obs.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []obs.Event
	for _, e := range c.events {
		if e.Kind == kind && e.Phase == phase {
			out = append(out, e)
		}
	}
	return out
}

// TestTaskHandle_ProgressReporting is a binding acceptance criterion: a
// long-running handler reports progress through a TaskHandle, observable by a
// tasks/get poller (RFC §8.4).
func TestTaskHandle_ProgressReporting(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	reported := make(chan struct{})
	release := make(chan struct{})
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		if err := h.Progress(ctx, 0.5, "halfway"); err != nil {
			return nil, err
		}
		close(reported)
		<-release
		return json.RawMessage(`{"isError":false}`), nil
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := res.Task.ID

	<-reported
	// tasks/get must observe the progress message.
	getRaw, err := e.Dispatch(ctx, MethodGet, mustTaskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	polled, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(getRaw)
	if err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if polled.Status != protocolcodec.TaskWorking {
		t.Fatalf("task left working during progress reporting: %q", polled.Status)
	}
	if polled.StatusMessage == "" {
		t.Fatal("progress did not update the task's status message")
	}
	close(release)
	if _, err := e.Dispatch(ctx, MethodResult, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("tasks/result: %v", err)
	}
}

// TestTaskHandle_ProgressEmitsObsEvent is a binding acceptance criterion for
// the bridge View-side task-progress channel (D-171): TaskHandle.Progress
// emits an obs/v1 task.progress PhaseProgress event carrying the clamped
// fraction, and TaskHandle.Status emits one with the message and no fraction —
// the stream the inspector forwards to the App preview (RFC §8.4).
func TestTaskHandle_ProgressEmitsObsEvent(t *testing.T) {
	t.Parallel()
	capE := &captureEmitter{}
	e := newEngine(t, &Options{Obs: capE})
	ctx := context.Background()

	done := make(chan struct{})
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		if err := h.Progress(ctx, 0.62, "halfway"); err != nil {
			return nil, err
		}
		if err := h.Status(ctx, "reticulating splines"); err != nil {
			return nil, err
		}
		close(done)
		return json.RawMessage(`{"isError":false}`), nil
	}
	if _, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	<-done

	// The terminal end event may still be racing; the two PhaseProgress events
	// are emitted synchronously inside the handler before `done`, so they are
	// present now.
	progress := capE.byKindPhase(obs.KindTaskProgress, obs.PhaseProgress)
	if len(progress) != 2 {
		t.Fatalf("want 2 PhaseProgress task.progress events, got %d", len(progress))
	}

	var withFrac obs.TaskProgressPayload
	if err := json.Unmarshal(progress[0].Payload, &withFrac); err != nil {
		t.Fatalf("decode progress payload: %v", err)
	}
	if withFrac.Fraction == nil || *withFrac.Fraction != 0.62 {
		t.Errorf("Progress fraction = %v, want 0.62", withFrac.Fraction)
	}
	if withFrac.Message != "halfway" {
		t.Errorf("Progress message = %q, want %q", withFrac.Message, "halfway")
	}

	var statusOnly obs.TaskProgressPayload
	if err := json.Unmarshal(progress[1].Payload, &statusOnly); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	if statusOnly.Fraction != nil {
		t.Errorf("Status must carry no fraction, got %v", *statusOnly.Fraction)
	}
	if statusOnly.Message != "reticulating splines" {
		t.Errorf("Status message = %q", statusOnly.Message)
	}
}

// TestTaskHandle_ProgressOnTerminalTaskEmitsNothing is a binding acceptance
// criterion (D-171): a Progress call on a task that has left working returns
// an error and emits no obs event.
func TestTaskHandle_ProgressOnTerminalTaskEmitsNothing(t *testing.T) {
	t.Parallel()
	capE := &captureEmitter{}
	e := newEngine(t, &Options{Obs: capE})
	ctx := context.Background()

	// Grab a handle from inside a handler, cancel the task, then prove a
	// Progress call after the task left working errors and emits nothing.
	gotHandle := make(chan TaskHandle, 1)
	release := make(chan struct{})
	handler := func(_ context.Context, h TaskHandle) (json.RawMessage, error) {
		gotHandle <- h
		<-release
		return json.RawMessage(`{"isError":false}`), nil
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	h := <-gotHandle
	if _, err := e.Dispatch(ctx, MethodCancel, mustTaskIDParams(t, res.Task.ID)); err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}

	before := len(capE.byKindPhase(obs.KindTaskProgress, obs.PhaseProgress))
	if err := h.Progress(context.Background(), 0.9, "too late"); err == nil {
		t.Error("Progress on a cancelled task must return an error")
	}
	after := len(capE.byKindPhase(obs.KindTaskProgress, obs.PhaseProgress))
	if after != before {
		t.Errorf("Progress on a terminal task emitted %d events, want 0", after-before)
	}
	close(release)
}

// TestTaskHandle_CooperativeCancellation is a binding acceptance criterion: a
// long handler observes tasks/cancel through ctx and the handle, unwinds
// cleanly, and the task ends cancelled — never a forced kill (RFC §8.4;
// brief 02 §4.7).
func TestTaskHandle_CooperativeCancellation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	started := make(chan struct{})
	unwound := make(chan struct{})
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		close(started)
		// Loop until cooperative cancellation is observed.
		for {
			select {
			case <-ctx.Done():
				close(unwound)
				return nil, ctx.Err()
			case <-time.After(time.Millisecond):
				if h.Cancelled() {
					close(unwound)
					return nil, errors.New("cancelled")
				}
			}
		}
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	id := res.Task.ID

	<-started
	if _, err := e.Dispatch(ctx, MethodCancel, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	select {
	case <-unwound:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not observe cooperative cancellation")
	}
	// The task ends cancelled.
	getRaw, err := e.Dispatch(ctx, MethodGet, mustTaskIDParams(t, id))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	final, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(getRaw)
	if final.Status != protocolcodec.TaskCancelled {
		t.Fatalf("cancelled task ended in %q, want cancelled", final.Status)
	}
}

// TestTaskHandle_RequireInput drives the input_required elicitation round-trip:
// the handler pauses for input, the task moves to input_required, the engine
// delivers a reply, the handler resumes (RFC §8.4).
func TestTaskHandle_RequireInput(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	resumed := make(chan InputResponse, 1)
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		resp, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
		if err != nil {
			return nil, err
		}
		resumed <- resp
		return json.RawMessage(`{"isError":false}`), nil
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	id := res.Task.ID

	// Wait for the task to reach input_required.
	deadline := time.After(2 * time.Second)
	for {
		prompt, ok := e.PendingInput(id)
		if ok {
			if prompt.Message != "approve?" {
				t.Fatalf("pending prompt = %q, want %q", prompt.Message, "approve?")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("task never reached input_required")
		case <-time.After(time.Millisecond):
		}
	}
	getRaw, _ := e.Dispatch(ctx, MethodGet, mustTaskIDParams(t, id))
	polled, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeGetTaskResult(getRaw)
	if polled.Status != protocolcodec.TaskInputRequired {
		t.Fatalf("task status during elicitation = %q, want input_required", polled.Status)
	}

	// Supply the input — the handler resumes.
	if err := e.SupplyInput(ctx, id, InputResponse{Data: []byte(`{"approved":true}`)}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	select {
	case resp := <-resumed:
		if string(resp.Data) != `{"approved":true}` {
			t.Fatalf("handler resumed with %q, want the supplied input", resp.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not resume after SupplyInput")
	}
	if _, err := e.Dispatch(ctx, MethodResult, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("tasks/result after elicitation: %v", err)
	}
}

type pausedInputTransitionStore struct {
	TaskStore
	transitioned chan struct{}
	release      chan struct{}
}

func (s *pausedInputTransitionStore) Transition(
	ctx context.Context, id string, to protocolcodec.TaskStatus, msg string,
) (TaskRecord, error) {
	rec, err := s.TaskStore.Transition(ctx, id, to, msg)
	if err == nil && to == protocolcodec.TaskInputRequired {
		close(s.transitioned)
		select {
		case <-s.release:
		case <-ctx.Done():
			return TaskRecord{}, ctx.Err()
		}
	}
	return rec, err
}

func TestTaskHandle_RequireInputPublishesPendingBeforeDurableStatus(t *testing.T) {
	store := &pausedInputTransitionStore{
		TaskStore: NewInMemoryStore(), transitioned: make(chan struct{}), release: make(chan struct{}),
	}
	e, err := NewEngine(store, &Options{
		Logger: quietLogger(), GenerateID: func() (string, error) { return "input-ready", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	resumed := make(chan InputResponse, 1)
	if _, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "input",
		Handle: func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
			resp, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
			if err == nil {
				resumed <- resp
			}
			return json.RawMessage(`{}`), err
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	<-store.transitioned
	rec, err := store.Get(context.Background(), "input-ready")
	if err != nil || rec.Status != protocolcodec.TaskInputRequired {
		t.Fatalf("durable status = %q, err = %v", rec.Status, err)
	}
	want := InputResponse{Data: []byte(`true`)}
	if err := e.SupplyInput(context.Background(), "input-ready", want); err != nil {
		t.Fatalf("SupplyInput while transition return is paused: %v", err)
	}
	if err := e.SupplyInput(context.Background(), "input-ready", want); !errors.Is(err, ErrNoPendingInput) {
		t.Fatalf("second SupplyInput error = %v, want ErrNoPendingInput", err)
	}
	close(store.release)
	select {
	case got := <-resumed:
		if string(got.Data) != string(want.Data) {
			t.Fatalf("resumed input = %s, want %s", got.Data, want.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not resume from input supplied during paused transition")
	}
}

type failingInputTransitionStore struct{ TaskStore }

func (s *failingInputTransitionStore) Transition(
	ctx context.Context, id string, to protocolcodec.TaskStatus, msg string,
) (TaskRecord, error) {
	if to == protocolcodec.TaskInputRequired {
		return TaskRecord{}, errors.New("input transition failed")
	}
	return s.TaskStore.Transition(ctx, id, to, msg)
}

func TestTaskHandle_RequireInputRollsBackPendingOnTransitionFailure(t *testing.T) {
	store := &failingInputTransitionStore{TaskStore: NewInMemoryStore()}
	e, err := NewEngine(store, &Options{
		Logger: quietLogger(), GenerateID: func() (string, error) { return "input-rollback", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	if _, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "input",
		Handle: func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
			_, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
			done <- err
			return nil, err
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RequireInput succeeded despite transition failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequireInput did not return after transition failure")
	}
	if _, ok := e.PendingInput("input-rollback"); ok {
		t.Fatal("failed input transition retained pending elicitation")
	}
}

// TestSupplyInput_NoPendingElicitation proves SupplyInput on a task with no
// outstanding elicitation is a typed error, not a panic.
func TestSupplyInput_NoPendingElicitation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()
	release := make(chan struct{})
	defer close(release)
	id := taskIDOf(t, e, blockingRun(release, []byte(`{}`), nil))
	if err := e.SupplyInput(ctx, id, InputResponse{}); !errors.Is(err, ErrNoPendingInput) {
		t.Fatalf("want ErrNoPendingInput, got %v", err)
	}
	// An unknown task is ErrTaskNotFound.
	if err := e.SupplyInput(ctx, "task_unknown", InputResponse{}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
}

// TestCreateForToolCall_RejectsBothAndNeitherHandler proves the engine rejects
// a CreateToolCallParams with both or neither of Run and Handle set.
func TestCreateForToolCall_RejectsBothAndNeitherHandler(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()
	// Neither.
	if _, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("neither Run nor Handle: want ErrInvalidParams, got %v", err)
	}
	// Both.
	_, err := e.CreateForToolCall(ctx, CreateToolCallParams{
		ToolName: "x",
		Run:      instantRun([]byte(`{}`), nil),
		Handle:   func(context.Context, TaskHandle) (json.RawMessage, error) { return []byte(`{}`), nil },
	})
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("both Run and Handle: want ErrInvalidParams, got %v", err)
	}
}

// TestTaskHandle_RequireInputCancelled proves RequireInput unblocks with an
// error when the task is cancelled while waiting for input.
func TestTaskHandle_RequireInputCancelled(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	finished := make(chan error, 1)
	handler := func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
		_, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
		finished <- err
		return nil, err
	}
	raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{ToolName: "x", Handle: handler})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	res, _ := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeCreateTaskResult(raw)
	id := res.Task.ID

	deadline := time.After(2 * time.Second)
	for {
		if _, ok := e.PendingInput(id); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("task never reached input_required")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := e.Dispatch(ctx, MethodCancel, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("RequireInput must error when the task is cancelled while waiting")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequireInput did not unblock on cancellation")
	}
}
