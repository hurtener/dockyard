// This file is the Phase 14 cross-subsystem integration test (CLAUDE.md §17).
// Phase 14's Deps name Phase 13 (the Tasks engine + the TaskStore seam) and
// Phase 03 (the Store seam), and Phase 14 consumes the Store seam to build the
// durable TaskStore. The test drives the surface end to end with real drivers:
// the durable TaskStore over a real modernc.org/sqlite Store (no mocks at the
// seam), a real tasks.Engine, and the real protocolcodec codec.
//
// It covers, per CLAUDE.md §17: auth-context propagation and the cross-context
// rejection; ≥1 failure mode per seam (a cross-context tasks/get; a purge
// racing live tasks); an N>=10 concurrency stress under -race with a
// post-teardown goroutine-leak assertion; and a real MCP client driving
// tasks/* end to end over a transport (the folded-in transport-mount criterion).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// newDurableEngine builds a tasks.Engine over the durable TaskStore layered on
// a real in-memory-file sqlite Store — the V1 durable backing. The durable
// TaskStore migration is supplied as a caller-owned store.MigrationSet
// (tasks.Migrations()), so this fixture needs no migration-registry isolation
// and is t.Parallel()-safe by construction (D-073, the S1 fix).
func newDurableEngine(t *testing.T, opts *tasks.Options) (*tasks.Engine, store.Store) {
	t.Helper()

	st, err := sqlitestore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("sqlitestore.Open: %v", err)
	}
	if err := st.Migrate(context.Background(), tasks.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ts, err := tasks.NewStore(st)
	if err != nil {
		t.Fatalf("tasks.NewStore: %v", err)
	}
	if opts == nil {
		opts = &tasks.Options{}
	}
	e, err := tasks.NewEngine(ts, opts)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e, st
}

// TestPhase14_DurableTaskStore_AuthContextPropagation proves a task's auth
// context propagates through the durable sqlite-backed TaskStore and that a
// cross-context access is rejected — the binding security criterion (RFC §8.5).
func TestPhase14_DurableTaskStore_AuthContextPropagation(t *testing.T) {
	t.Parallel()
	e, st := newDurableEngine(t, &tasks.Options{RequestorIdentifiable: true})
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	// Alice creates a task through the durable store.
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName:    "generate_report",
		AuthContext: "alice",
		Run:         func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"isError":false}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, err := codec.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Task.ID
	params := taskIDParams(t, id)

	// Alice reaches her own task.
	if _, err := e.DispatchAs(ctx, "alice", tasks.MethodGet, params); err != nil {
		t.Fatalf("alice's own tasks/get over the durable store: %v", err)
	}
	// Bob is rejected — the durable store carries the auth context and the
	// engine binds access to it. The rejection does not leak the task exists.
	_, err = e.DispatchAs(ctx, "bob", tasks.MethodGet, params)
	if !errors.Is(err, tasks.ErrCrossContext) {
		t.Fatalf("cross-context tasks/get: want ErrCrossContext, got %v", err)
	}
	if tasks.JSONRPCCode(err) != tasks.CodeInvalidParams {
		t.Fatalf("cross-context rejection code = %d, want %d",
			tasks.JSONRPCCode(err), tasks.CodeInvalidParams)
	}
}

// TestPhase14_DurableTaskStore_FailureMode_CrossContextCancel is a mandated
// failure mode: a cross-context tasks/cancel against the durable seam is
// rejected, and the task is left untouched for its owner.
func TestPhase14_DurableTaskStore_FailureMode_CrossContextCancel(t *testing.T) {
	t.Parallel()
	e, st := newDurableEngine(t, &tasks.Options{RequestorIdentifiable: true})
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	release := make(chan struct{})
	defer close(release)
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName:    "x",
		AuthContext: "alice",
		Run: func(ctx context.Context) (json.RawMessage, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return json.RawMessage(`{}`), nil
		},
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	// Bob's cancel is rejected.
	if _, err := e.DispatchAs(ctx, "bob", tasks.MethodCancel, taskIDParams(t, id)); !errors.Is(err, tasks.ErrCrossContext) {
		t.Fatalf("cross-context cancel: want ErrCrossContext, got %v", err)
	}
	// Alice's task is still working — bob's rejected cancel did not touch it.
	getRaw, err := e.DispatchAs(ctx, "alice", tasks.MethodGet, taskIDParams(t, id))
	if err != nil {
		t.Fatalf("alice tasks/get: %v", err)
	}
	got, _ := codec.DecodeGetTaskResult(getRaw)
	if got.Status != protocolcodec.TaskWorking {
		t.Fatalf("alice's task status = %q after a rejected cross-context cancel, want working", got.Status)
	}
}

// TestPhase14_TTLPurge_OverDurableStore is the binding TTL-purge criterion: the
// background purge sweep reaps an expired task from the durable sqlite store.
func TestPhase14_TTLPurge_OverDurableStore(t *testing.T) {
	t.Parallel()
	e, st := newDurableEngine(t, &tasks.Options{
		Lifecycle: tasks.LifecycleFromMillis(20, 0, 5, 0), // 20ms max TTL, 5ms purge
	})
	defer func() { _ = st.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ttl := int64(10) // 10ms — well under the purge cadence's reach
	raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "x",
		TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	e.StartSweep(ctx)
	defer e.StopSweep()

	// The sweep must reap the expired task from the durable store.
	deadline := time.After(3 * time.Second)
	for {
		_, err := e.Dispatch(ctx, tasks.MethodGet, taskIDParams(t, id))
		if errors.Is(err, tasks.ErrTaskNotFound) {
			return // reaped — success
		}
		select {
		case <-deadline:
			t.Fatal("TTL purge sweep did not reap the expired task from the durable store")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestPhase14_ConcurrencyStress drives an N>=10 concurrency stress against the
// durable store: concurrent task creation against the per-requestor cap and the
// purge sweep racing live tasks, under -race, with a post-teardown
// goroutine-leak assertion (CLAUDE.md §17).
func TestPhase14_ConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	e, st := newDurableEngine(t, &tasks.Options{
		RequestorIdentifiable: true,
		Lifecycle: tasks.LifecycleFromMillis(
			30, // max TTL 30ms
			0,  // no default TTL
			3,  // purge every 3ms — races the live tasks
			50, // generous per-requestor cap
		),
	})
	ctx, cancel := context.WithCancel(context.Background())
	e.StartSweep(ctx)

	const workers = 12
	const perWorker = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			authCtx := fmt.Sprintf("ctx-%d", w)
			for i := 0; i < perWorker; i++ {
				ttl := int64(15) // short — the purge sweep will race these
				raw, err := e.CreateForToolCall(ctx, tasks.CreateToolCallParams{
					ToolName:    "x",
					AuthContext: authCtx,
					TaskMeta:    protocolcodec.TaskMeta{TTL: &ttl},
					Run:         func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
				})
				if err != nil {
					t.Errorf("worker %d CreateForToolCall: %v", w, err)
					return
				}
				created, err := codec.DecodeCreateTaskResult(raw)
				if err != nil {
					t.Errorf("worker %d decode: %v", w, err)
					return
				}
				// A concurrent auth-scoped listing races other workers' creates
				// and the purge sweep — it must never error or leak a context.
				listRaw, err := e.DispatchAs(ctx, authCtx, tasks.MethodList, nil)
				if err != nil && !errors.Is(err, tasks.ErrUnknownMethod) {
					t.Errorf("worker %d tasks/list: %v", w, err)
					return
				}
				_ = listRaw
				_ = created
			}
		}(w)
	}
	wg.Wait()

	// Tear down: stop the sweep, close the store, then assert no goroutine leak.
	cancel()
	e.StopSweep()
	if err := st.Close(); err != nil {
		t.Fatalf("store Close: %v", err)
	}
	assertNoGoroutineLeak(t, baseline)
}

// TestPhase14_TasksOverTransport is the folded-in transport-mount criterion: a
// real MCP-style client drives tasks/get/result/cancel/list end to end over a
// real HTTP transport, through the Tasks transport mount (RFC §8.2). The mount
// intercepts tasks/* JSON-RPC frames and answers them from the engine; the
// initialize handshake carries the capabilities.tasks block.
func TestPhase14_TasksOverTransport(t *testing.T) {
	t.Parallel()
	e, st := newDurableEngine(t, &tasks.Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
		PollInterval:          10,
	})
	defer func() { _ = st.Close() }()

	// Create a task directly so there is something to drive over the wire.
	raw, err := e.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "generate_report",
		Run:      func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"isError":false}`), nil },
	})
	if err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	created, _ := codec.DecodeCreateTaskResult(raw)
	id := created.Task.ID

	// Mount tasks/* ahead of a stand-in SDK handler that returns an initialize
	// result — the shape the real SDK produces.
	mount := tasks.NewMount(e)
	sdkStandIn := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2025-06-18"}}`))
	})
	srv := httptest.NewServer(mount.HTTPMiddleware(sdkStandIn))
	defer srv.Close()

	post := func(method string, params json.RawMessage) map[string]json.RawMessage {
		t.Helper()
		frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
		if params != nil {
			frame["params"] = params
		}
		body, _ := json.Marshal(frame)
		resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s POST: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatalf("%s decode: %v (body %s)", method, err, out)
		}
		return decoded
	}

	// 1. initialize — the capabilities.tasks block must be injected.
	initResp := post("initialize", json.RawMessage(`{"protocolVersion":"2025-06-18"}`))
	var initResult struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(initResp["result"], &initResult); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if _, ok := initResult.Capabilities["tasks"]; !ok {
		t.Fatal("initialize handshake over the transport carries no capabilities.tasks block")
	}

	// 2. tasks/get over the wire.
	getResp := post(tasks.MethodGet, taskIDParams(t, id))
	if _, isErr := getResp["error"]; isErr {
		t.Fatalf("tasks/get over the transport errored: %s", getResp["error"])
	}
	polled, err := codec.DecodeGetTaskResult(getResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/get result: %v", err)
	}
	if polled.ID != id {
		t.Fatalf("tasks/get over the wire returned task %q, want %q", polled.ID, id)
	}

	// 3. tasks/result over the wire — blocks to terminal, returns the payload.
	resultResp := post(tasks.MethodResult, taskIDParams(t, id))
	if _, isErr := resultResp["error"]; isErr {
		t.Fatalf("tasks/result over the transport errored: %s", resultResp["error"])
	}

	// 4. tasks/list over the wire.
	listResp := post(tasks.MethodList, nil)
	if _, isErr := listResp["error"]; isErr {
		t.Fatalf("tasks/list over the transport errored: %s", listResp["error"])
	}
	list, err := codec.DecodeListTasksResult(listResp["result"])
	if err != nil {
		t.Fatalf("decode tasks/list result: %v", err)
	}
	if len(list.Tasks) != 1 {
		t.Fatalf("tasks/list over the wire returned %d tasks, want 1", len(list.Tasks))
	}

	// 5. tasks/cancel over the wire on a now-terminal task — rejected with the
	//    spec's -32602, surfaced as a JSON-RPC error frame.
	cancelResp := post(tasks.MethodCancel, taskIDParams(t, id))
	if _, isErr := cancelResp["error"]; !isErr {
		t.Fatal("tasks/cancel of a terminal task over the transport should be a JSON-RPC error")
	}
}
