package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

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
