package protocolcodec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func modernTestTask(status TaskStatus) Task {
	return Task{ID: "task-modern", Status: status,
		CreatedAt:     time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
		LastUpdatedAt: time.Date(2026, 7, 28, 10, 1, 0, 0, time.UTC),
		TTL:           ptr(int64(60000)), PollInterval: ptr(int64(1000))}
}

func TestModernGoldenCreateTaskResult(t *testing.T) {
	raw, err := CodecFor(VersionMCP20260728).EncodeCreateTaskResult(CreateTaskResult{Task: modernTestTask(TaskWorking)})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"createdAt":"2026-07-28T10:00:00Z","lastUpdatedAt":"2026-07-28T10:01:00Z","pollIntervalMs":1000,"resultType":"task","status":"working","taskId":"task-modern","ttlMs":60000}`
	if got := canon(t, raw); got != want {
		t.Fatalf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestModernGoldenDetailedTasks(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	cases := []struct {
		name string
		task DetailedTask
		want string
	}{
		{"input", DetailedTask{Task: modernTestTask(TaskInputRequired), InputRequests: map[string]json.RawMessage{"approval": json.RawMessage(`{"method":"elicitation/create","params":{"message":"Approve?"}}`)}}, `{"createdAt":"2026-07-28T10:00:00Z","inputRequests":{"approval":{"method":"elicitation/create","params":{"message":"Approve?"}}},"lastUpdatedAt":"2026-07-28T10:01:00Z","pollIntervalMs":1000,"resultType":"complete","status":"input_required","taskId":"task-modern","ttlMs":60000}`},
		{"completed", DetailedTask{Task: modernTestTask(TaskCompleted), Result: map[string]any{"content": "ready"}}, `{"createdAt":"2026-07-28T10:00:00Z","lastUpdatedAt":"2026-07-28T10:01:00Z","pollIntervalMs":1000,"result":{"content":"ready"},"resultType":"complete","status":"completed","taskId":"task-modern","ttlMs":60000}`},
		{"failed", DetailedTask{Task: modernTestTask(TaskFailed), Error: map[string]any{"code": float64(-32603)}}, `{"createdAt":"2026-07-28T10:00:00Z","error":{"code":-32603},"lastUpdatedAt":"2026-07-28T10:01:00Z","pollIntervalMs":1000,"resultType":"complete","status":"failed","taskId":"task-modern","ttlMs":60000}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := c.EncodeDetailedTaskResult(tc.task)
			if err != nil {
				t.Fatal(err)
			}
			if got := canon(t, raw); got != tc.want {
				t.Fatalf("golden mismatch:\n got: %s\nwant: %s", got, tc.want)
			}
			if _, err := c.DecodeDetailedTaskResult(raw); err != nil {
				t.Fatalf("decode: %v", err)
			}
		})
	}
}

func TestModernGoldenUpdateAckAndCapability(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	raw, err := c.EncodeUpdateTaskParams(UpdateTaskParams{TaskID: "task-modern", InputResponses: map[string]json.RawMessage{"approval": json.RawMessage(`{"action":"accept"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"inputResponses":{"approval":{"action":"accept"}},"taskId":"task-modern"}`; canon(t, raw) != want {
		t.Fatalf("update = %s, want %s", canon(t, raw), want)
	}
	ack, err := c.EncodeTaskAck()
	if err != nil {
		t.Fatal(err)
	}
	if string(ack) != `{"resultType":"complete"}` {
		t.Fatalf("ack = %s", ack)
	}
	if err := c.DecodeTaskAck(ack); err != nil {
		t.Fatal(err)
	}
	capability, err := c.EncodeTasksServerCapability(TasksServerCapability{List: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(capability) != `{}` {
		t.Fatalf("capability = %s", capability)
	}
}

func TestModernRejectsLegacyTaskAPIs(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	checks := []func() error{
		func() error { _, err := c.EncodeTaskMeta(TaskMeta{}); return err },
		func() error { _, _, err := c.DecodeTaskMeta(json.RawMessage(`{"ttl":1}`)); return err },
		func() error { _, err := c.EncodeRelatedTaskMeta(nil, "task"); return err },
		func() error { _, _, err := c.DecodeRelatedTaskMeta(nil); return err },
		func() error { _, err := c.EncodeCreateTaskResultMeta(nil, CreateTaskResultMeta{}); return err },
		func() error { _, _, err := c.DecodeCreateTaskResultMeta(nil); return err },
		func() error { _, err := c.EncodeListTasksParams(ListTasksParams{}); return err },
		func() error { _, err := c.DecodeListTasksParams(nil); return err },
		func() error { _, err := c.EncodeListTasksResult(ListTasksResult{}); return err },
		func() error { _, err := c.DecodeListTasksResult(nil); return err },
		func() error { _, err := c.DecodeSupplyInputParams(json.RawMessage(`{"taskId":"x"}`)); return err },
	}
	for _, check := range checks {
		err := check()
		if !errors.Is(err, ErrUnsupportedOperation) {
			t.Errorf("got %v, want ErrUnsupportedOperation", err)
		}
	}
}

func TestModernTaskRoundTripAndMalformedFields(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	task := modernTestTask(TaskWorking)
	raw, err := c.EncodeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.DecodeTask(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.Status != task.Status || !got.CreatedAt.Equal(task.CreatedAt) || !got.LastUpdatedAt.Equal(task.LastUpdatedAt) {
		t.Fatalf("round trip = %#v, want %#v", got, task)
	}

	invalidTasks := []struct {
		name string
		raw  string
		want string
	}{
		{"invalid JSON", `{`, "task:"},
		{"missing ttl", `{"taskId":"t","status":"working","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"2026-07-28T00:00:01Z"}`, "ttlMs is required"},
		{"empty ID", `{"taskId":"","status":"working","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"2026-07-28T00:00:01Z","ttlMs":null}`, "taskId is required"},
		{"invalid status", `{"taskId":"t","status":"unknown","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"2026-07-28T00:00:01Z","ttlMs":null}`, `unknown status "unknown"`},
		{"invalid created", `{"taskId":"t","status":"working","createdAt":"bad","lastUpdatedAt":"2026-07-28T00:00:01Z","ttlMs":null}`, "createdAt"},
		{"invalid updated", `{"taskId":"t","status":"working","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"bad","ttlMs":null}`, "lastUpdatedAt"},
	}
	for _, tc := range invalidTasks {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.DecodeTask(json.RawMessage(tc.raw))
			if !errors.Is(err, ErrMalformedMeta) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want ErrMalformedMeta containing %q", err, tc.want)
			}
		})
	}

	for _, invalid := range []Task{{}, modernTestTask(TaskStatus("unknown"))} {
		if _, err := c.EncodeTask(invalid); !errors.Is(err, ErrMalformedMeta) {
			t.Errorf("EncodeTask(%#v) error = %v, want ErrMalformedMeta", invalid, err)
		}
	}
}

func TestModernCapabilitySchema(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		if _, present, err := c.DecodeTasksServerCapability(raw); err != nil || present {
			t.Fatalf("DecodeTasksServerCapability(%s) = present %v, error %v", raw, present, err)
		}
	}
	capability, present, err := c.DecodeTasksServerCapability(json.RawMessage(`{}`))
	if err != nil || !present || !capability.Cancel || !capability.ToolsCall {
		t.Fatalf("empty capability = %#v, present %v, error %v", capability, present, err)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`{"list":true}`)} {
		if _, _, err := c.DecodeTasksServerCapability(raw); !errors.Is(err, ErrMalformedMeta) {
			t.Errorf("DecodeTasksServerCapability(%s) error = %v, want ErrMalformedMeta", raw, err)
		}
	}
}

func TestModernDetailedTaskRejectsInvalidShapes(t *testing.T) {
	c := CodecFor(VersionMCP20260728)
	cases := []struct {
		name string
		task DetailedTask
		want string
	}{
		{"input requests absent", DetailedTask{Task: modernTestTask(TaskInputRequired)}, "requires inputRequests"},
		{"empty input key", DetailedTask{Task: modernTestTask(TaskInputRequired), InputRequests: map[string]json.RawMessage{"": json.RawMessage(`{"method":"roots/list","params":{}}`)}}, "key is empty"},
		{"invalid input request", DetailedTask{Task: modernTestTask(TaskInputRequired), InputRequests: map[string]json.RawMessage{"x": json.RawMessage(`{"method":"roots/list"}`)}}, `input request "x"`},
		{"result absent", DetailedTask{Task: modernTestTask(TaskCompleted)}, "requires result"},
		{"error absent", DetailedTask{Task: modernTestTask(TaskFailed)}, "requires error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.EncodeDetailedTaskResult(tc.task)
			if !errors.Is(err, ErrMalformedMeta) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want ErrMalformedMeta containing %q", err, tc.want)
			}
		})
	}

	base := `"taskId":"t","status":"completed","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"2026-07-28T00:00:01Z","ttlMs":null,"result":{}`
	for _, tc := range []struct{ raw, want string }{
		{`{`, "task:"},
		{`{` + base + `}`, `resultType must be "complete"`},
		{`{"resultType":"complete",` + base + `,"taskId":7}`, "DetailedTask:"},
	} {
		if _, err := c.DecodeDetailedTaskResult(json.RawMessage(tc.raw)); !errors.Is(err, ErrMalformedMeta) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("DecodeDetailedTaskResult(%s) error = %v, want %q", tc.raw, err, tc.want)
		}
	}
}

func TestModernInputUnionStructuralValidation(t *testing.T) {
	t.Parallel()
	requests := []struct {
		name, method, raw string
		valid             bool
	}{
		{"elicitation", CoreMethodElicitation, `{"method":"elicitation/create","params":{"message":"Approve?"}}`, true},
		{"sampling", CoreMethodSampling, `{"method":"sampling/createMessage","params":{"messages":[],"maxTokens":10}}`, true},
		{"roots", CoreMethodRoots, `{"method":"roots/list","params":{}}`, true},
		{"method mismatch", CoreMethodRoots, `{"method":"elicitation/create","params":{"message":"Approve?"}}`, false},
		{"unknown method", "", `{"method":"tasks/get","params":{}}`, false},
		{"missing elicitation message", CoreMethodElicitation, `{"method":"elicitation/create","params":{}}`, false},
		{"missing sampling messages", CoreMethodSampling, `{"method":"sampling/createMessage","params":{"maxTokens":10}}`, false},
		{"non-object params", CoreMethodRoots, `{"method":"roots/list","params":[]}`, false},
	}
	for _, tc := range requests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModernInputRequest(tc.method, json.RawMessage(tc.raw))
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateModernInputRequest() error = %v, valid = %v", err, tc.valid)
			}
		})
	}

	responses := []struct {
		name, method, raw string
		valid             bool
	}{
		{"elicitation", CoreMethodElicitation, `{"action":"accept"}`, true},
		{"sampling", CoreMethodSampling, `{"role":"assistant","content":{"type":"text","text":"ok"}}`, true},
		{"roots", CoreMethodRoots, `{"roots":[]}`, true},
		{"valid JSON wrong union member", CoreMethodRoots, `{"action":"accept"}`, false},
		{"sampling missing content", CoreMethodSampling, `{"role":"assistant"}`, false},
		{"roots wrong type", CoreMethodRoots, `{"roots":{}}`, false},
	}
	for _, tc := range responses {
		t.Run("response "+tc.name, func(t *testing.T) {
			err := ValidateModernInputResponse(tc.method, json.RawMessage(tc.raw))
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateModernInputResponse() error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestLegacyTaskGoldenRemainsLegacy(t *testing.T) {
	raw, err := CodecFor(VersionMCP20251125).EncodeTask(modernTestTask(TaskWorking))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["ttl"]; !ok {
		t.Error("legacy ttl missing")
	}
	if _, ok := fields["ttlMs"]; ok {
		t.Error("legacy emitted ttlMs")
	}
}

func FuzzDecodeModernDetailedTask(f *testing.F) {
	f.Add([]byte(`{"resultType":"complete","taskId":"t","status":"completed","createdAt":"2026-07-28T00:00:00Z","lastUpdatedAt":"2026-07-28T00:00:01Z","ttlMs":null,"result":{}}`))
	f.Add([]byte(`{"resultType":"task","taskId":"t"}`))
	f.Add([]byte(`null`))
	c := CodecFor(VersionMCP20260728)
	f.Fuzz(func(t *testing.T, raw []byte) {
		d, err := c.DecodeDetailedTaskResult(raw)
		if err != nil {
			return
		}
		encoded, err := c.EncodeDetailedTaskResult(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.DecodeDetailedTaskResult(encoded); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzDecodeModernUpdateParams(f *testing.F) {
	f.Add([]byte(`{"taskId":"t","inputResponses":{"x":{"action":"accept"}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	c := CodecFor(VersionMCP20260728)
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := c.DecodeUpdateTaskParams(raw)
		if err != nil {
			return
		}
		encoded, err := c.EncodeUpdateTaskParams(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.DecodeUpdateTaskParams(encoded); err != nil {
			t.Fatal(err)
		}
	})
}
