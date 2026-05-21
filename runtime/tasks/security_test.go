package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestDispatchAs_CrossContextRejected is a binding acceptance criterion: a task
// created under one auth context is not reachable via tasks/get|result|cancel
// from another (RFC §8.5; brief 02 §4.5).
func TestDispatchAs_CrossContextRejected(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	id := mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	params := mustTaskIDParams(t, id)

	for _, method := range []string{MethodGet, MethodResult, MethodCancel} {
		t.Run(method, func(t *testing.T) {
			// Bob may not reach alice's task.
			_, err := e.DispatchAs(ctx, "bob", method, params)
			if !errors.Is(err, ErrCrossContext) {
				t.Fatalf("%s cross-context: want ErrCrossContext, got %v", method, err)
			}
			// The rejection must be indistinguishable from "not found": same
			// JSON-RPC code, and the message must not reveal the task exists.
			if code := JSONRPCCode(err); code != CodeInvalidParams {
				t.Fatalf("%s cross-context code = %d, want %d", method, code, CodeInvalidParams)
			}
		})
	}
}

// TestDispatchAs_OwnContextAllowed proves the owning auth context still reaches
// its own task through DispatchAs.
func TestDispatchAs_OwnContextAllowed(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	id := mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	if _, err := e.DispatchAs(ctx, "alice", MethodGet, mustTaskIDParams(t, id)); err != nil {
		t.Fatalf("alice's own tasks/get rejected: %v", err)
	}
}

// TestDispatchAs_ListScopedToCaller proves tasks/list returns only the caller's
// own tasks, never another context's (RFC §8.5).
func TestDispatchAs_ListScopedToCaller(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	_ = mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	_ = mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	_ = mustCreateAuth(t, e, "bob", instantRun([]byte(`{}`), nil))

	raw, err := e.DispatchAs(ctx, "alice", MethodList, nil)
	if err != nil {
		t.Fatalf("tasks/list: %v", err)
	}
	list, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).DecodeListTasksResult(raw)
	if err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Tasks) != 2 {
		t.Fatalf("alice's tasks/list returned %d tasks, want 2 (scope leak)", len(list.Tasks))
	}
}

// TestListWithheld_WhenRequestorNotIdentifiable is a binding acceptance
// criterion: tasks/list is not advertised and not served when the engine
// cannot identify requestors — the unauthenticated single-user stdio case
// (RFC §8.5; brief 02 §4.5).
func TestListWithheld_WhenRequestorNotIdentifiable(t *testing.T) {
	t.Parallel()
	// AdvertiseList is opted in, but RequestorIdentifiable is false.
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:        quietLogger(),
		AdvertiseList: true,
		// RequestorIdentifiable defaults false — unauthenticated stdio.
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// The capability must NOT advertise list.
	if e.Capability().List {
		t.Fatal("tasks/list advertised despite an unidentifiable requestor")
	}
	// Dispatch must refuse to serve tasks/list.
	if _, err := e.Dispatch(context.Background(), MethodList, nil); !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("tasks/list served when withheld: %v", err)
	}
	// DispatchAs must also refuse it.
	if _, err := e.DispatchAs(context.Background(), "", MethodList, nil); !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("DispatchAs served tasks/list when withheld: %v", err)
	}
}

// TestListAdvertised_OnlyWhenBothOptedAndIdentifiable proves the capability is
// advertised exactly when both AdvertiseList and RequestorIdentifiable hold.
func TestListAdvertised_OnlyWhenBothOptedAndIdentifiable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		advertise, identifiable, want bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{true, true, true},
	}
	for _, tc := range cases {
		e, err := NewEngine(NewInMemoryStore(), &Options{
			Logger:                quietLogger(),
			AdvertiseList:         tc.advertise,
			RequestorIdentifiable: tc.identifiable,
		})
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		if got := e.Capability().List; got != tc.want {
			t.Errorf("advertise=%v identifiable=%v: List=%v, want %v",
				tc.advertise, tc.identifiable, got, tc.want)
		}
	}
}

// TestDispatchAs_MissingTaskIsNotFound proves a genuinely-missing task and a
// cross-context task are reported identically — no existence leak.
func TestDispatchAs_MissingTaskIsNotFound(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{
		Logger:                quietLogger(),
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	missing := mustTaskIDParams(t, "task_deadbeefdeadbeefdeadbeefdeadbeef")
	_, missingErr := e.DispatchAs(ctx, "bob", MethodGet, missing)

	id := mustCreateAuth(t, e, "alice", instantRun([]byte(`{}`), nil))
	_, crossErr := e.DispatchAs(ctx, "bob", MethodGet, mustTaskIDParams(t, id))

	if JSONRPCCode(missingErr) != JSONRPCCode(crossErr) {
		t.Fatalf("missing-task code %d differs from cross-context code %d — existence leak",
			JSONRPCCode(missingErr), JSONRPCCode(crossErr))
	}
}

// TestCryptoIDs_Are128BitAndUnique reconfirms the Phase 13 crypto-strong ID
// generator: every engine-issued task ID is 128 bits of crypto/rand entropy and
// unique (RFC §8.5; brief 02 §4.5).
func TestCryptoIDs_Are128BitAndUnique(t *testing.T) {
	t.Parallel()
	e, err := NewEngine(NewInMemoryStore(), &Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		raw, err := e.CreateForToolCall(context.Background(), CreateToolCallParams{
			ToolName: "x", Run: instantRun([]byte(`{}`), nil),
		})
		if err != nil {
			t.Fatalf("CreateForToolCall: %v", err)
		}
		var res struct {
			Task struct {
				ID string `json:"taskId"`
			} `json:"task"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		id := res.Task.ID
		if seen[id] {
			t.Fatalf("duplicate task ID issued: %q", id)
		}
		seen[id] = true
		// "task_" prefix + 32 hex chars = 128-bit entropy.
		if len(id) != len("task_")+2*idBytes {
			t.Fatalf("task ID %q has unexpected length %d", id, len(id))
		}
	}
}
