package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

func TestModernInputLifecyclePersistsAndResumes(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, err := NewEngine(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := workingRecord("modern-input")
	rec.AuthContext = "alice"
	if err := store.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	got := make(chan TaskInputResponse, 1)
	go func() {
		resp, err := e.RequestInput(context.Background(), rec.ID, InputRequest{
			Key: "approval", Method: InputMethodElicitation, Payload: json.RawMessage(`{"method":"elicitation/create","params":{"message":"Approve?"}}`),
		})
		if err == nil {
			got <- resp
		}
	}()
	waitForStatus(t, store, rec.ID, protocolcodec.TaskInputRequired)

	result, err := e.DispatchModern(context.Background(), "alice", MethodGet, ModernRequest{TaskID: rec.ID})
	if err != nil || result.Task == nil || len(result.Task.InputRequests) != 1 {
		t.Fatalf("modern get: result=%#v err=%v", result, err)
	}
	response := TaskInputResponse{Payload: json.RawMessage(`{"action":"accept"}`)}
	if _, err := e.DispatchModern(context.Background(), "alice", MethodUpdate, ModernRequest{
		TaskID: rec.ID, InputResponses: map[string]TaskInputResponse{"unknown": response, "approval": response},
	}); err != nil {
		t.Fatalf("modern update: %v", err)
	}
	select {
	case resumed := <-got:
		if string(resumed.Payload) != string(response.Payload) {
			t.Fatalf("resumed payload = %s", resumed.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not resume")
	}
	persisted, err := store.Get(context.Background(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != protocolcodec.TaskWorking || len(persisted.InputRequests) != 0 || len(persisted.InputResponses) != 1 {
		t.Fatalf("persisted state = %#v", persisted)
	}
	if err := store.AddInputRequest(context.Background(), rec.ID, InputRequest{
		Key: "approval", Method: InputMethodElicitation, Payload: json.RawMessage(`{}`),
	}); !errors.Is(err, ErrDuplicateInputKey) {
		t.Fatalf("reused key error = %v", err)
	}
}

func TestDispatchModernAuthAndMethodSet(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, _ := NewEngine(store, nil)
	rec := workingRecord("modern-auth")
	rec.AuthContext = "alice"
	_ = store.Create(context.Background(), rec)
	for _, method := range []string{MethodList, MethodResult, MethodSupplyInput} {
		if _, err := e.DispatchModern(context.Background(), "alice", method, ModernRequest{TaskID: rec.ID}); !errors.Is(err, ErrUnknownMethod) {
			t.Errorf("%s error = %v, want ErrUnknownMethod", method, err)
		}
	}
	for _, method := range []string{MethodGet, MethodUpdate, MethodCancel} {
		req := ModernRequest{TaskID: rec.ID, InputResponses: map[string]TaskInputResponse{}}
		if _, err := e.DispatchModern(context.Background(), "mallory", method, req); !errors.Is(err, ErrCrossContext) {
			t.Errorf("%s cross-context error = %v", method, err)
		}
	}
}

func TestDispatchModernWireUsesModernCodec(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, _ := NewEngine(store, nil)
	rec := workingRecord("modern-wire")
	rec.AuthContext = "alice"
	_ = store.Create(context.Background(), rec)
	if err := store.AddInputRequest(context.Background(), rec.ID, InputRequest{
		Key: "approval", Method: InputMethodElicitation,
		Payload: json.RawMessage(`{"method":"elicitation/create","params":{"message":"Approve?"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := e.DispatchModernWire(context.Background(), "alice", MethodGet, json.RawMessage(`{"taskId":"modern-wire"}`))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := protocolcodec.CodecFor(protocolcodec.VersionMCP20260728).DecodeDetailedTaskResult(raw)
	if err != nil {
		t.Fatalf("decode modern get: %v (%s)", err, raw)
	}
	if detail.Status != protocolcodec.TaskInputRequired || len(detail.InputRequests) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	ack, err := e.DispatchModernWire(context.Background(), "alice", MethodUpdate,
		json.RawMessage(`{"taskId":"modern-wire","inputResponses":{"approval":{"action":"accept"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := protocolcodec.CodecFor(protocolcodec.VersionMCP20260728).DecodeTaskAck(ack); err != nil {
		t.Fatalf("decode ack: %v (%s)", err, ack)
	}
}

func TestModernUpdateRejectsResponseForWrongUnionMember(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, _ := NewEngine(store, nil)
	rec := workingRecord("modern-wrong-union")
	rec.AuthContext = "alice"
	_ = store.Create(context.Background(), rec)
	if err := store.AddInputRequest(context.Background(), rec.ID, InputRequest{
		Key: "roots", Method: InputMethodRoots,
		Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.DispatchModern(context.Background(), "alice", MethodUpdate, ModernRequest{
		TaskID: rec.ID, InputResponses: map[string]TaskInputResponse{
			"roots": {Payload: json.RawMessage(`{"action":"accept"}`)},
		},
	})
	if !errors.Is(err, ErrInvalidParams) || JSONRPCCode(err) != CodeInvalidParams {
		t.Fatalf("error = %v, code = %d; want invalid params", err, JSONRPCCode(err))
	}
}

func TestModernUpdateConcurrentReuse(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	e, _ := NewEngine(store, nil)
	rec := workingRecord("modern-concurrent")
	rec.AuthContext = "alice"
	_ = store.Create(context.Background(), rec)
	if err := store.AddInputRequest(context.Background(), rec.ID, InputRequest{
		Key: "once", Method: InputMethodRoots, Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
	}); err != nil {
		t.Fatal(err)
	}

	const callers = 32
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, err := e.DispatchModern(context.Background(), "alice", MethodUpdate, ModernRequest{
				TaskID:         rec.ID,
				InputResponses: map[string]TaskInputResponse{"once": {Payload: json.RawMessage(`{"roots":[]}`)}},
			})
			if err != nil {
				t.Errorf("update: %v", err)
			}
		}()
	}
	wg.Wait()
	got, err := store.Get(context.Background(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.InputResponses) != 1 || len(got.InputRequests) != 0 {
		t.Fatalf("concurrent state = %#v", got)
	}
}

func waitForStatus(t *testing.T, store TaskStore, id string, want protocolcodec.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := store.Get(context.Background(), id)
		if err == nil && rec.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %q did not reach %q", id, want)
}
