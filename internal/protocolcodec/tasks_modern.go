package protocolcodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

const modernResultTask = "task"
const modernResultComplete = "complete"

// modernCodec is deliberately separate from v1Codec: the Tasks extension is
// not wire-compatible across the two core protocol versions.
type modernCodec struct{ v1Codec }

var _ Codec = modernCodec{}

func (modernCodec) Version() ProtocolVersion { return VersionMCP20260728 }

type modernTaskWire struct {
	TaskID        string                      `json:"taskId"`
	Status        TaskStatus                  `json:"status"`
	StatusMessage string                      `json:"statusMessage,omitempty"`
	CreatedAt     string                      `json:"createdAt"`
	LastUpdatedAt string                      `json:"lastUpdatedAt"`
	TTL           *int64                      `json:"ttlMs"`
	PollInterval  *int64                      `json:"pollIntervalMs,omitempty"`
	ResultType    string                      `json:"resultType,omitempty"`
	InputRequests *map[string]json.RawMessage `json:"inputRequests,omitempty"`
	Result        *map[string]any             `json:"result,omitempty"`
	Error         *map[string]any             `json:"error,omitempty"`
	Meta          Meta                        `json:"_meta,omitempty"`
}

func modernTaskToWire(t Task) modernTaskWire {
	return modernTaskWire{TaskID: t.ID, Status: t.Status, StatusMessage: t.StatusMessage,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano), LastUpdatedAt: t.LastUpdatedAt.UTC().Format(time.RFC3339Nano),
		TTL: t.TTL, PollInterval: t.PollInterval}
}

func modernTaskFromWire(w modernTaskWire) (Task, error) {
	if w.TaskID == "" {
		return Task{}, fmt.Errorf("%w: task: taskId is required", ErrMalformedMeta)
	}
	if !w.Status.Valid() {
		return Task{}, fmt.Errorf("%w: task: unknown status %q", ErrMalformedMeta, w.Status)
	}
	created, err := parseTaskTime("createdAt", w.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	updated, err := parseTaskTime("lastUpdatedAt", w.LastUpdatedAt)
	if err != nil {
		return Task{}, err
	}
	return Task{ID: w.TaskID, Status: w.Status, StatusMessage: w.StatusMessage, CreatedAt: created,
		LastUpdatedAt: updated, TTL: w.TTL, PollInterval: w.PollInterval}, nil
}

func (modernCodec) EncodeTask(t Task) (json.RawMessage, error) {
	if t.ID == "" || !t.Status.Valid() || t.CreatedAt.IsZero() || t.LastUpdatedAt.IsZero() {
		return nil, fmt.Errorf("%w: task: required field is missing or invalid", ErrMalformedMeta)
	}
	return json.Marshal(modernTaskToWire(t))
}

func (modernCodec) DecodeTask(raw json.RawMessage) (Task, error) {
	if err := requireModernTaskFields(raw); err != nil {
		return Task{}, err
	}
	var w modernTaskWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Task{}, fmt.Errorf("%w: task: %w", ErrMalformedMeta, err)
	}
	return modernTaskFromWire(w)
}

func (modernCodec) EncodeTaskMeta(TaskMeta) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: task request augmentation", ErrUnsupportedOperation)
}
func (modernCodec) DecodeTaskMeta(json.RawMessage) (TaskMeta, bool, error) {
	return TaskMeta{}, false, fmt.Errorf("%w: task request augmentation", ErrUnsupportedOperation)
}
func (modernCodec) EncodeRelatedTaskMeta(Meta, string) (Meta, error) {
	return nil, fmt.Errorf("%w: related-task metadata", ErrUnsupportedOperation)
}
func (modernCodec) DecodeRelatedTaskMeta(Meta) (string, bool, error) {
	return "", false, fmt.Errorf("%w: related-task metadata", ErrUnsupportedOperation)
}
func (modernCodec) EncodeCreateTaskResultMeta(Meta, CreateTaskResultMeta) (Meta, error) {
	return nil, fmt.Errorf("%w: create-task metadata", ErrUnsupportedOperation)
}
func (modernCodec) DecodeCreateTaskResultMeta(Meta) (CreateTaskResultMeta, bool, error) {
	return CreateTaskResultMeta{}, false, fmt.Errorf("%w: create-task metadata", ErrUnsupportedOperation)
}

func (modernCodec) EncodeTasksServerCapability(TasksServerCapability) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (modernCodec) DecodeTasksServerCapability(raw json.RawMessage) (TasksServerCapability, bool, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return TasksServerCapability{}, false, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return TasksServerCapability{}, false, fmt.Errorf("%w: extensions[%q]: %w", ErrMalformedMeta, ExtensionTasks, err)
	}
	if len(object) != 0 {
		return TasksServerCapability{}, false, fmt.Errorf("%w: extensions[%q] must be an empty object", ErrMalformedMeta, ExtensionTasks)
	}
	return TasksServerCapability{Cancel: true, ToolsCall: true}, true, nil
}

func (modernCodec) EncodeCreateTaskResult(r CreateTaskResult) (json.RawMessage, error) {
	w := modernTaskToWire(r.Task)
	w.ResultType, w.Meta = modernResultTask, r.Meta
	return json.Marshal(w)
}
func (modernCodec) DecodeCreateTaskResult(raw json.RawMessage) (CreateTaskResult, error) {
	if err := requireModernTaskFields(raw); err != nil {
		return CreateTaskResult{}, err
	}
	var w modernTaskWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return CreateTaskResult{}, fmt.Errorf("%w: CreateTaskResult: %w", ErrMalformedMeta, err)
	}
	if w.ResultType != modernResultTask {
		return CreateTaskResult{}, fmt.Errorf("%w: CreateTaskResult.resultType must be %q", ErrMalformedMeta, modernResultTask)
	}
	t, err := modernTaskFromWire(w)
	if err != nil {
		return CreateTaskResult{}, err
	}
	return CreateTaskResult{Task: t, Meta: w.Meta}, nil
}

func (c modernCodec) EncodeGetTaskResult(t Task) (json.RawMessage, error) {
	return c.EncodeDetailedTaskResult(DetailedTask{Task: t})
}
func (c modernCodec) DecodeGetTaskResult(raw json.RawMessage) (Task, error) {
	d, err := c.DecodeDetailedTaskResult(raw)
	return d.Task, err
}

func (modernCodec) EncodeDetailedTaskResult(d DetailedTask) (json.RawMessage, error) {
	w := modernTaskToWire(d.Task)
	w.ResultType = modernResultComplete
	if d.InputRequests != nil {
		w.InputRequests = &d.InputRequests
	}
	if d.Result != nil {
		w.Result = &d.Result
	}
	if d.Error != nil {
		w.Error = &d.Error
	}
	if err := validateDetailedTask(w); err != nil {
		return nil, err
	}
	return json.Marshal(w)
}
func (modernCodec) DecodeDetailedTaskResult(raw json.RawMessage) (DetailedTask, error) {
	if err := requireModernTaskFields(raw); err != nil {
		return DetailedTask{}, err
	}
	var w modernTaskWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return DetailedTask{}, fmt.Errorf("%w: DetailedTask: %w", ErrMalformedMeta, err)
	}
	if w.ResultType != modernResultComplete {
		return DetailedTask{}, fmt.Errorf("%w: DetailedTask.resultType must be %q", ErrMalformedMeta, modernResultComplete)
	}
	if err := validateDetailedTask(w); err != nil {
		return DetailedTask{}, err
	}
	t, err := modernTaskFromWire(w)
	if err != nil {
		return DetailedTask{}, err
	}
	d := DetailedTask{Task: t}
	if w.InputRequests != nil {
		d.InputRequests = *w.InputRequests
	}
	if w.Result != nil {
		d.Result = *w.Result
	}
	if w.Error != nil {
		d.Error = *w.Error
	}
	return d, nil
}

func requireModernTaskFields(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("%w: task: %w", ErrMalformedMeta, err)
	}
	for _, name := range []string{"taskId", "status", "createdAt", "lastUpdatedAt", "ttlMs"} {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("%w: task: %s is required", ErrMalformedMeta, name)
		}
	}
	return nil
}
func validateDetailedTask(w modernTaskWire) error {
	switch w.Status {
	case TaskInputRequired:
		if w.InputRequests == nil {
			return fmt.Errorf("%w: input_required task requires inputRequests", ErrMalformedMeta)
		}
		for key, raw := range *w.InputRequests {
			if key == "" {
				return fmt.Errorf("%w: input request key is empty", ErrMalformedMeta)
			}
			if err := ValidateModernInputRequest("", raw); err != nil {
				return fmt.Errorf("%w: input request %q: %w", ErrMalformedMeta, key, err)
			}
		}
	case TaskCompleted:
		if w.Result == nil {
			return fmt.Errorf("%w: completed task requires result", ErrMalformedMeta)
		}
	case TaskFailed:
		if w.Error == nil {
			return fmt.Errorf("%w: failed task requires error", ErrMalformedMeta)
		}
	}
	return nil
}

// ValidateModernInputRequest validates one member of the modern InputRequest
// union and, when declaredMethod is non-empty, verifies its discriminator.
func ValidateModernInputRequest(declaredMethod string, raw json.RawMessage) error {
	var request struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Method == "" || len(request.Params) == 0 {
		return fmt.Errorf("input request must contain method and params")
	}
	if declaredMethod != "" && request.Method != declaredMethod {
		return fmt.Errorf("declared method %q does not match payload method %q", declaredMethod, request.Method)
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil {
		return fmt.Errorf("%s params must be an object", request.Method)
	}
	switch request.Method {
	case CoreMethodElicitation:
		if !nonEmptyJSONString(params["message"]) {
			return fmt.Errorf("%s params.message is required", request.Method)
		}
	case CoreMethodSampling:
		var messages []json.RawMessage
		if err := json.Unmarshal(params["messages"], &messages); err != nil {
			return fmt.Errorf("%s params.messages is required and must be an array", request.Method)
		}
		var maxTokens float64
		if err := json.Unmarshal(params["maxTokens"], &maxTokens); err != nil || maxTokens <= 0 {
			return fmt.Errorf("%s params.maxTokens must be positive", request.Method)
		}
	case CoreMethodRoots:
	default:
		return fmt.Errorf("unsupported input request method %q", request.Method)
	}
	return nil
}

// ValidateModernInputResponse validates one member of the modern InputResponse
// union according to the method of its matching persisted InputRequest.
func ValidateModernInputResponse(method string, raw json.RawMessage) error {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return fmt.Errorf("%s result must be an object", method)
	}
	switch method {
	case CoreMethodElicitation:
		var action string
		if json.Unmarshal(result["action"], &action) != nil || (action != "accept" && action != "decline" && action != "cancel") {
			return fmt.Errorf("%s result.action is invalid", method)
		}
	case CoreMethodSampling:
		var role string
		if json.Unmarshal(result["role"], &role) != nil || role != "assistant" {
			return fmt.Errorf("%s result.role must be assistant", method)
		}
		if content, ok := result["content"]; !ok || bytes.Equal(content, []byte("null")) {
			return fmt.Errorf("%s result.content is required", method)
		}
	case CoreMethodRoots:
		var roots []struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(result["roots"], &roots); err != nil {
			return fmt.Errorf("%s result.roots is required and must be an array", method)
		}
		for _, root := range roots {
			if root.URI == "" {
				return fmt.Errorf("%s result root URI is required", method)
			}
		}
	default:
		return fmt.Errorf("unsupported input response method %q", method)
	}
	return nil
}

func nonEmptyJSONString(raw json.RawMessage) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value != ""
}

func (modernCodec) EncodeUpdateTaskParams(p UpdateTaskParams) (json.RawMessage, error) {
	if p.TaskID == "" {
		return nil, fmt.Errorf("%w: tasks/update: taskId is required", ErrMalformedMeta)
	}
	if p.InputResponses == nil {
		return nil, fmt.Errorf("%w: tasks/update: inputResponses is required", ErrMalformedMeta)
	}
	return json.Marshal(struct {
		TaskID         string                     `json:"taskId"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}{p.TaskID, p.InputResponses})
}
func (modernCodec) DecodeUpdateTaskParams(raw json.RawMessage) (UpdateTaskParams, error) {
	var w struct {
		TaskID         string                     `json:"taskId"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return UpdateTaskParams{}, fmt.Errorf("%w: tasks/update: %w", ErrMalformedMeta, err)
	}
	if w.TaskID == "" || w.InputResponses == nil {
		return UpdateTaskParams{}, fmt.Errorf("%w: tasks/update requires taskId and inputResponses", ErrMalformedMeta)
	}
	return UpdateTaskParams{TaskID: w.TaskID, InputResponses: w.InputResponses}, nil
}

func (modernCodec) EncodeTaskAck() (json.RawMessage, error) {
	return json.RawMessage(`{"resultType":"complete"}`), nil
}
func (modernCodec) DecodeTaskAck(raw json.RawMessage) error {
	var w struct {
		ResultType string `json:"resultType"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("%w: task acknowledgement: %w", ErrMalformedMeta, err)
	}
	if w.ResultType != modernResultComplete {
		return fmt.Errorf("%w: task acknowledgement.resultType must be %q", ErrMalformedMeta, modernResultComplete)
	}
	return nil
}

func (modernCodec) EncodeListTasksParams(ListTasksParams) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: tasks/list", ErrUnsupportedOperation)
}
func (modernCodec) DecodeListTasksParams(json.RawMessage) (ListTasksParams, error) {
	return ListTasksParams{}, fmt.Errorf("%w: tasks/list", ErrUnsupportedOperation)
}
func (modernCodec) EncodeListTasksResult(ListTasksResult) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: tasks/list", ErrUnsupportedOperation)
}
func (modernCodec) DecodeListTasksResult(json.RawMessage) (ListTasksResult, error) {
	return ListTasksResult{}, fmt.Errorf("%w: tasks/list", ErrUnsupportedOperation)
}
func (modernCodec) DecodeSupplyInputParams(json.RawMessage) (SupplyInputParams, error) {
	return SupplyInputParams{}, fmt.Errorf("%w: dockyard/tasks/supplyInput", ErrUnsupportedOperation)
}
