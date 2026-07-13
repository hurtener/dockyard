package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestDispatch_SupplyInput_HappyPath drives the Dockyard-internal
// `dockyard/tasks/supplyInput` method end-to-end through Engine.Dispatch (the
// wire half of D-134). It proves the handleSupplyInput dispatch handler
// decodes the wire shape, hands the InputResponse to Engine.SupplyInput, the
// handler resumes with the supplied data, and the dispatcher returns the
// empty `{}` envelope so the caller sees a JSON-RPC OK.
func TestDispatch_SupplyInput_HappyPath(t *testing.T) {
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

	// Wait for the task to reach input_required so the elicitation is pending.
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

	params := mustSupplyInputParamsRaw(t, id, []byte(`{"approved":true}`), false)
	out, err := e.Dispatch(ctx, MethodSupplyInput, params)
	if err != nil {
		t.Fatalf("Dispatch(supplyInput): %v", err)
	}
	if string(out) != `{}` {
		t.Fatalf("Dispatch(supplyInput) result = %q, want {}", out)
	}

	select {
	case resp := <-resumed:
		if string(resp.Data) != `{"approved":true}` {
			t.Fatalf("handler resumed with data %q, want the wire-supplied JSON", resp.Data)
		}
		if resp.Declined {
			t.Fatal("handler resumed with Declined=true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not resume after Dispatch(supplyInput)")
	}
}

// TestDispatch_SupplyInput_Declined proves the wire `declined: true` flag
// reaches the handler so a Reject decision propagates exactly like a server-
// side SupplyInput call would.
func TestDispatch_SupplyInput_Declined(t *testing.T) {
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

	params := mustSupplyInputParamsRaw(t, id, nil, true)
	if _, err := e.Dispatch(ctx, MethodSupplyInput, params); err != nil {
		t.Fatalf("Dispatch(supplyInput, declined): %v", err)
	}

	select {
	case resp := <-resumed:
		if !resp.Declined {
			t.Fatal("handler resumed with Declined=false, want true")
		}
		if len(resp.Data) != 0 {
			t.Fatalf("handler resumed with Data=%q, want empty when declined", resp.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not resume after Dispatch(supplyInput, declined)")
	}
}

// TestDispatch_SupplyInput_MalformedParams proves a malformed JSON envelope
// is rejected as a typed JSON-RPC ErrInvalidParams (caller sees an honest
// error, not a panic, never a leak across the boundary — CLAUDE.md §13).
func TestDispatch_SupplyInput_MalformedParams(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	// Not JSON at all.
	if _, err := e.Dispatch(ctx, MethodSupplyInput, json.RawMessage(`not-json`)); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("non-JSON params: err = %v, want ErrInvalidParams", err)
	}
	// JSON with no taskId — the codec rejects this.
	if _, err := e.Dispatch(ctx, MethodSupplyInput, json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("missing taskId: err = %v, want ErrInvalidParams", err)
	}
}

// TestDispatch_SupplyInput_UnknownTask proves a supplyInput against a task ID
// the store does not know surfaces as ErrTaskNotFound, not a panic and not a
// silent success.
func TestDispatch_SupplyInput_UnknownTask(t *testing.T) {
	t.Parallel()
	e := newEngine(t, nil)
	ctx := context.Background()

	params := mustSupplyInputParamsRaw(t, "task_does_not_exist", []byte(`{"approved":true}`), false)
	_, err := e.Dispatch(ctx, MethodSupplyInput, params)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("unknown task: err = %v, want ErrTaskNotFound", err)
	}
}

// TestDispatch_SupplyInput_NullDataIsEmpty proves the dispatch handler treats
// the JSON literal `null` for `data` the same as an absent field — the
// resulting InputResponse carries an empty Data slice.
func TestDispatch_SupplyInput_NullDataIsEmpty(t *testing.T) {
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

	// Build the wire shape with data: null explicitly.
	payload := fmt.Sprintf(`{"taskId":%q,"data":null}`, id)
	if _, err := e.Dispatch(ctx, MethodSupplyInput, json.RawMessage(payload)); err != nil {
		t.Fatalf("Dispatch(supplyInput, data:null): %v", err)
	}

	select {
	case resp := <-resumed:
		if len(resp.Data) != 0 {
			t.Fatalf("handler resumed with Data=%q, want empty when wire data is null", resp.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not resume after Dispatch(supplyInput, data:null)")
	}
}

type blockingResumeStore struct {
	TaskStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingResumeStore) Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (TaskRecord, error) {
	if to == protocolcodec.TaskWorking && msg == "input received, resuming" {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return TaskRecord{}, ctx.Err()
		}
	}
	return s.TaskStore.Transition(ctx, id, to, msg)
}

func TestSupplyInputClaimsElicitationOnce(t *testing.T) {
	base := NewInMemoryStore()
	store := &blockingResumeStore{TaskStore: base, entered: make(chan struct{}), release: make(chan struct{})}
	e, err := NewEngine(store, &Options{Logger: quietLogger(), GenerateID: func() (string, error) { return "one-shot", nil }})
	if err != nil {
		t.Fatal(err)
	}
	created, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "legacy-input",
		Handle: func(ctx context.Context, h TaskHandle) (json.RawMessage, error) {
			_, err := h.RequireInput(ctx, InputPrompt{Message: "approve?"})
			return nil, err
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := e.PendingInput(created.ID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("elicitation was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	if err := e.SupplyInput(context.Background(), created.ID, InputResponse{Data: []byte(`true`)}); err != nil {
		t.Fatal(err)
	}
	<-store.entered
	if err := e.SupplyInput(context.Background(), created.ID, InputResponse{Data: []byte(`false`)}); !errors.Is(err, ErrNoPendingInput) {
		t.Fatalf("second SupplyInput error = %v, want ErrNoPendingInput", err)
	}
	close(store.release)
}

func TestLegacyInputWaitObservesTaskCancellationWithBackgroundContext(t *testing.T) {
	store := NewInMemoryStore()
	e, err := NewEngine(store, &Options{Logger: quietLogger(), GenerateID: func() (string, error) { return "legacy-cancel", nil }})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	created, err := e.CreateToolTask(context.Background(), CreateToolCallParams{
		ToolName: "legacy-input",
		Handle: func(_ context.Context, h TaskHandle) (json.RawMessage, error) {
			_, err := h.RequireInput(context.Background(), InputPrompt{Message: "approve?"})
			done <- err
			return nil, err
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, created.ID, protocolcodec.TaskInputRequired)
	if _, err := e.Dispatch(context.Background(), MethodCancel, mustTaskIDParams(t, created.ID)); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.PendingInput(created.ID); ok {
		t.Fatal("cancelled task retained a pending legacy elicitation")
	}
	if err := e.SupplyInput(context.Background(), created.ID, InputResponse{Data: []byte(`true`)}); !errors.Is(err, ErrNoPendingInput) {
		t.Fatalf("SupplyInput after cancellation error = %v, want ErrNoPendingInput", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled legacy wait returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled legacy background-context wait did not return")
	}
}

// mustSupplyInputParamsRaw builds the on-wire `supplyInputParams` envelope —
// `{taskId, data?, declined?}` — matching the schema the codec decodes. Tests
// drive the dispatch path, so we shape the JSON by hand (the codec exposes a
// decoder only; the on-wire schema is stable and small).
func mustSupplyInputParamsRaw(t *testing.T, id string, data json.RawMessage, declined bool) json.RawMessage {
	t.Helper()
	out := map[string]any{"taskId": id}
	if len(data) > 0 {
		out["data"] = data
	}
	if declined {
		out["declined"] = true
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal supplyInputParams: %v", err)
	}
	return raw
}
