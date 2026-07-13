package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// MethodUpdate exists only in the modern Tasks lifecycle. The legacy Dispatch
// method intentionally does not route it.
const MethodUpdate = protocolcodec.ModernMethodUpdate

// ModernRequest is the codec-independent request accepted by DispatchModern.
// A protocol codec decodes taskId and inputResponses before crossing this seam.
type ModernRequest struct {
	TaskID         string
	InputResponses map[string]TaskInputResponse
}

// ModernDetailedTask is the codec-independent status-specific tasks/get value.
// Exactly the field appropriate to Status is populated.
type ModernDetailedTask struct {
	Task          protocolcodec.Task
	InputRequests map[string]InputRequest
	Result        json.RawMessage
	Error         string
}

// ModernResult is a codec-independent modern result. Get carries Task; update
// and cancel are empty acknowledgements. The codec adds resultType:"complete".
type ModernResult struct {
	Task *ModernDetailedTask
}

// DispatchModernWire is the protocolcodec-backed adapter intended for the
// SDK's AddReceivingCustomMethod handlers. It is deliberately separate from
// legacy Dispatch so selecting a modern protocol version cannot expose legacy
// tasks/list, tasks/result, or dockyard/tasks/supplyInput.
func (e *Engine) DispatchModernWire(ctx context.Context, authContext, method string, params json.RawMessage) (json.RawMessage, error) {
	codec := protocolcodec.CodecFor(protocolcodec.VersionMCP20260728)
	var req ModernRequest
	switch method {
	case MethodGet, MethodCancel:
		p, err := codec.DecodeTaskIDParams(params)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidParams, err)
		}
		req.TaskID = p.ID
	case MethodUpdate:
		p, err := codec.DecodeUpdateTaskParams(params)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidParams, err)
		}
		req.TaskID = p.TaskID
		req.InputResponses = make(map[string]TaskInputResponse, len(p.InputResponses))
		for key, payload := range p.InputResponses {
			req.InputResponses[key] = TaskInputResponse{Payload: payload}
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownMethod, method)
	}
	result, err := e.DispatchModern(ctx, authContext, method, req)
	if err != nil {
		return nil, err
	}
	if method != MethodGet {
		return codec.EncodeTaskAck()
	}
	detail := protocolcodec.DetailedTask{Task: result.Task.Task}
	if result.Task.InputRequests != nil {
		detail.InputRequests = make(map[string]json.RawMessage, len(result.Task.InputRequests))
		for key, input := range result.Task.InputRequests {
			detail.InputRequests[key] = input.Payload
		}
	}
	if result.Task.Result != nil {
		if err := json.Unmarshal(result.Task.Result, &detail.Result); err != nil {
			return nil, fmt.Errorf("%w: completed task result is not an object: %w", ErrInvalidParams, err)
		}
	}
	if result.Task.Error != "" {
		detail.Error = map[string]any{"code": CodeInternalError, "message": result.Task.Error}
	}
	return codec.EncodeDetailedTaskResult(detail)
}

// DispatchModern routes only the modern extension methods. tasks/list,
// tasks/result, and dockyard/tasks/supplyInput are method-not-found here.
// authContext is checked independently on every request.
func (e *Engine) DispatchModern(ctx context.Context, authContext, method string, req ModernRequest) (ModernResult, error) {
	switch method {
	case MethodGet, MethodUpdate, MethodCancel:
	default:
		return ModernResult{}, fmt.Errorf("%w: %q", ErrUnknownMethod, method)
	}
	if req.TaskID == "" {
		return ModernResult{}, fmt.Errorf("%w: taskId is required", ErrInvalidParams)
	}
	rec, err := e.authorizedRecord(ctx, authContext, req.TaskID)
	if err != nil {
		return ModernResult{}, err
	}
	switch method {
	case MethodGet:
		detail := modernDetail(rec)
		return ModernResult{Task: &detail}, nil
	case MethodUpdate:
		if req.InputResponses == nil {
			return ModernResult{}, fmt.Errorf("%w: inputResponses is required", ErrInvalidParams)
		}
		for key, resp := range req.InputResponses {
			input, pending := rec.InputRequests[key]
			if key == "" || !pending {
				continue
			}
			if err := protocolcodec.ValidateModernInputRequest(string(input.Method), input.Payload); err != nil {
				return ModernResult{}, fmt.Errorf("%w: invalid persisted input request %q: %w", ErrInvalidParams, key, err)
			}
			if err := protocolcodec.ValidateModernInputResponse(string(input.Method), resp.Payload); err != nil {
				return ModernResult{}, fmt.Errorf("%w: invalid input response %q", ErrInvalidParams, key)
			}
		}
		runtime, err := e.taskRuntimeFor(ctx, req.TaskID)
		if err != nil {
			return ModernResult{}, err
		}
		accepted, _, err := e.store.ApplyInputResponses(ctx, req.TaskID, req.InputResponses)
		if err != nil {
			return ModernResult{}, err
		}
		runtime.deliverModernInputs(req.TaskID, accepted)
		return ModernResult{}, nil
	case MethodCancel:
		if err := e.cancelModern(ctx, rec); err != nil {
			return ModernResult{}, err
		}
		return ModernResult{}, nil
	}
	return ModernResult{}, fmt.Errorf("%w: %q", ErrUnknownMethod, method)
}

func (e *Engine) authorizedRecord(ctx context.Context, authContext, id string) (TaskRecord, error) {
	rec, err := e.store.Get(ctx, id)
	if err != nil {
		return TaskRecord{}, err
	}
	if rec.AuthContext != authContext {
		e.log.WarnContext(ctx, "cross-context task access rejected", slog.String("taskId", id))
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrCrossContext, id)
	}
	return rec, nil
}

func modernDetail(rec TaskRecord) ModernDetailedTask {
	d := ModernDetailedTask{Task: rec.Task()}
	switch rec.Status {
	case protocolcodec.TaskInputRequired:
		d.InputRequests = rec.InputRequests
	case protocolcodec.TaskCompleted:
		d.Result = append(json.RawMessage(nil), rec.Result.Payload...)
	case protocolcodec.TaskFailed:
		d.Error = rec.Result.Err
	}
	return d
}

func (e *Engine) cancelModern(ctx context.Context, rec TaskRecord) error {
	if rec.Status.IsTerminal() {
		return fmt.Errorf("%w: cannot cancel task %q already in terminal status %q", ErrAlreadyTerminal, rec.ID, rec.Status)
	}
	final, applied, err := e.cancelTask(ctx, rec.ID,
		"The task was cancelled by request.", TaskResult{Err: "task cancelled"}, taskOwnerForIdentity(e.identity, rec.ID))
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("%w: cannot cancel task %q already in terminal status %q", ErrAlreadyTerminal, rec.ID, final.Status)
	}
	return nil
}

// RequestInput persists a modern mid-flight input request and blocks until its
// matching response has been durably accepted or ctx is cancelled.
func (e *Engine) RequestInput(ctx context.Context, taskID string, req InputRequest) (TaskInputResponse, error) {
	if err := e.store.AddInputRequest(ctx, taskID, req); err != nil {
		return TaskInputResponse{}, err
	}
	ch := make(chan TaskInputResponse, 1)
	e.mu.Lock()
	byKey := e.inputWaiters[taskID]
	if byKey == nil {
		byKey = make(map[string][]chan TaskInputResponse)
		e.inputWaiters[taskID] = byKey
	}
	byKey[req.Key] = append(byKey[req.Key], ch)
	e.mu.Unlock()

	// Close the registration race: update may have committed before the waiter
	// was installed. Persistence, not the channel, remains authoritative.
	if rec, err := e.store.Get(ctx, taskID); err == nil {
		if resp, ok := rec.InputResponses[req.Key]; ok {
			e.removeModernInputWaiter(taskID, req.Key, ch)
			return resp, nil
		}
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		e.removeModernInputWaiter(taskID, req.Key, ch)
		return TaskInputResponse{}, fmt.Errorf("%w: input_required wait cancelled: %w", ErrInvalidParams, ctx.Err())
	}
}

func (e *Engine) removeModernInputWaiter(taskID, key string, target chan TaskInputResponse) {
	e.mu.Lock()
	defer e.mu.Unlock()
	waiters := e.inputWaiters[taskID][key]
	for i, waiter := range waiters {
		if waiter == target {
			waiters = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(waiters) == 0 {
		delete(e.inputWaiters[taskID], key)
	} else {
		e.inputWaiters[taskID][key] = waiters
	}
	if len(e.inputWaiters[taskID]) == 0 {
		delete(e.inputWaiters, taskID)
	}
}

func (e *Engine) deliverModernInputs(taskID string, accepted map[string]TaskInputResponse) {
	e.mu.Lock()
	var deliveries []struct {
		ch   chan TaskInputResponse
		resp TaskInputResponse
	}
	for key, resp := range accepted {
		for _, ch := range e.inputWaiters[taskID][key] {
			deliveries = append(deliveries, struct {
				ch   chan TaskInputResponse
				resp TaskInputResponse
			}{ch, resp})
		}
		delete(e.inputWaiters[taskID], key)
	}
	if len(e.inputWaiters[taskID]) == 0 {
		delete(e.inputWaiters, taskID)
	}
	e.mu.Unlock()
	for _, d := range deliveries {
		d.ch <- d.resp
	}
}
