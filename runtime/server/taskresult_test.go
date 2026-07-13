package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

type taskToolInput struct{}
type taskToolOutput struct {
	Message string `json:"message"`
}

type failingTaskToolOutput struct{}

func (failingTaskToolOutput) MarshalJSON() ([]byte, error) {
	return nil, errors.New("intentional output marshal failure")
}

type blockingDeleteStore struct {
	tasks.TaskStore
	entered chan struct{}
}

type blockingAdmissionGetStore struct {
	tasks.TaskStore
	gets atomic.Int32
}

func (s *blockingAdmissionGetStore) Get(ctx context.Context, id string) (tasks.TaskRecord, error) {
	if s.gets.Add(1) == 1 {
		<-ctx.Done()
		return tasks.TaskRecord{}, ctx.Err()
	}
	return s.TaskStore.Get(ctx, id)
}

func (s *blockingDeleteStore) Delete(ctx context.Context, _ string) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestModernToolsCallReturnsFlatCreateTaskResultOverSDKHTTP(t *testing.T) {
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		GenerateID: func() (string, error) { return "created-over-sdk", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-result", Version: "1"}, &server.Options{
		Tasks:            engine,
		TasksAuthContext: func(*http.Request) string { return "principal-a" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start", AuthContext: tasks.RequestAuthContext(ctx),
				Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"content":[]}`), nil },
			}, true)
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	var response struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	if string(response.Result["resultType"]) != `"task"` || string(response.Result["taskId"]) != `"created-over-sdk"` {
		t.Fatalf("tools/call result = %s, want flat CreateTaskResult", raw)
	}
	if _, nested := response.Result["task"]; nested {
		t.Fatalf("modern CreateTaskResult unexpectedly uses legacy task wrapper: %s", raw)
	}
	if _, err := engine.DispatchModern(context.Background(), "principal-b", tasks.MethodGet,
		tasks.ModernRequest{TaskID: "created-over-sdk"}); err == nil {
		t.Fatal("created task was not bound to request auth context")
	}
}

func TestModernRequiredTaskRejectsMissingClientCapability(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, _ := tasks.NewEngine(store, nil)
	s, _ := server.New(server.Info{Name: "task-required", Version: "1"}, &server.Options{Tasks: engine})
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				}}, true)
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, _ := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	ts := httptest.NewServer(h)
	defer ts.Close()
	raw := modernToolCall(t, ts, `{}`)
	if !strings.Contains(string(raw), `"code":-32021`) || !strings.Contains(string(raw), `"io.modelcontextprotocol/tasks"`) {
		t.Fatalf("response = %s, want missing-required-client-capability error", raw)
	}
	var response struct {
		Error struct {
			Data struct {
				RequiredCapabilities map[string]json.RawMessage `json:"requiredCapabilities"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	caps := response.Error.Data.RequiredCapabilities
	if len(caps) != 1 || caps["extensions"] == nil || caps["roots"] != nil {
		t.Fatalf("required capabilities = %s, want extensions only", caps)
	}
	assertRejectedTaskDidNotStart(t, started, store)
}

func TestModernOptionalTaskWithoutCapabilityReturnsFallbackWithoutStarting(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, _ := tasks.NewEngine(store, nil)
	s, _ := server.New(server.Info{Name: "task-optional", Version: "1"}, &server.Options{Tasks: engine})
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				}}, false)
			return server.ToolOutput[taskToolOutput]{
				Text: "fallback", Structured: taskToolOutput{Message: "fallback"}, CreatedTask: &created,
			}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, _ := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	ts := httptest.NewServer(h)
	defer ts.Close()
	raw := modernToolCall(t, ts, `{}`)
	if strings.Contains(string(raw), `"resultType":"task"`) || !strings.Contains(string(raw), "fallback") {
		t.Fatalf("optional unsupported response = %s, want ordinary fallback", raw)
	}
	if !strings.Contains(string(raw), `"resultType":"complete"`) {
		t.Fatalf("optional fallback response = %s, want complete result", raw)
	}
	assertRejectedTaskDidNotStart(t, started, store)
}

func TestTaskDoesNotStartWhenSDKOutputMarshalFails(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, _ := tasks.NewEngine(store, nil)
	s, _ := server.New(server.Info{Name: "task-output-marshal", Version: "1"}, &server.Options{Tasks: engine})
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[failingTaskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				}}, true)
			return server.ToolOutput[failingTaskToolOutput]{CreatedTask: &created}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, _ := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if !strings.Contains(string(raw), "intentional output marshal failure") {
		t.Fatalf("response = %s, want output marshal failure", raw)
	}
	assertRejectedTaskDidNotStart(t, started, store)
}

func TestToolOutputRejectsCreatedTaskCombinedWithCoreMRTR(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, _ := tasks.NewEngine(store, &tasks.Options{GenerateID: func() (string, error) { return "mixed", nil }})
	s, _ := server.New(server.Info{Name: "mixed-task-mrtr", Version: "1"}, &server.Options{Tasks: engine})
	if err := server.AddToolWithSchemasMRTR(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ server.ToolCall[taskToolInput]) (server.ToolOutput[taskToolOutput], error) {
			created, err := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start", Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			return server.ToolOutput[taskToolOutput]{
				CreatedTask: &created,
				InputRequests: map[string]server.InputRequest{
					"approval": server.ElicitationRequest{Message: "Approve?"},
				},
			}, err
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if !strings.Contains(string(raw), "cannot combine core MRTR continuation with CreatedTask") {
		t.Fatalf("mixed task/MRTR response = %s", raw)
	}
	if strings.Contains(string(raw), `"resultType":"task"`) {
		t.Fatalf("mixed task/MRTR output was encoded as a task: %s", raw)
	}
	assertRejectedTaskDidNotStart(t, started, store)
}

func TestTaskAdmissionAbortBoundsBlockingDelete(t *testing.T) {
	base := tasks.NewInMemoryStore()
	store := &blockingDeleteStore{TaskStore: base, entered: make(chan struct{}, 1)}
	started := make(chan struct{}, 1)
	ids := []string{"blocked-delete", "after-blocked-delete"}
	nextID := 0
	engine, err := tasks.NewEngine(store, &tasks.Options{
		RequestorIdentifiable: true,
		Lifecycle:             tasks.Lifecycle{MaxConcurrentPerRequestor: 1},
		GenerateID: func() (string, error) {
			id := ids[nextID]
			nextID++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-blocked-delete", Version: "1"}, &server.Options{Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			_, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			if createErr != nil {
				return server.ToolOutput[taskToolOutput]{}, createErr
			}
			return server.ToolOutput[taskToolOutput]{}, errors.New("reject task after creation")
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	startedAt := time.Now()
	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("blocking admission cleanup took %v", elapsed)
	}
	select {
	case <-store.entered:
	default:
		t.Fatal("admission cleanup did not call Delete")
	}
	if !strings.Contains(string(raw), "context deadline exceeded") {
		t.Fatalf("response = %s, want surfaced cleanup timeout", raw)
	}
	select {
	case <-started:
		t.Fatal("aborted task handler started")
	default:
	}
	rec, err := base.Get(context.Background(), "blocked-delete")
	if err != nil {
		t.Fatalf("aborted task record: %v", err)
	}
	if rec.Status != "cancelled" {
		t.Fatalf("aborted task status = %q, want cancelled", rec.Status)
	}
	release := make(chan struct{})
	defer close(release)
	if _, err := engine.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "after", Run: func(context.Context) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatalf("aborted task retained concurrency slot: %v", err)
	}
}

func TestTaskAdmissionRollbackGetsFreshBoundAfterBlockingGet(t *testing.T) {
	base := tasks.NewInMemoryStore()
	store := &blockingAdmissionGetStore{TaskStore: base}
	started := make(chan struct{}, 1)
	ids := []string{"blocked-admission", "replacement"}
	var nextID atomic.Int32
	engine, err := tasks.NewEngine(store, &tasks.Options{
		RequestorIdentifiable: true,
		Lifecycle:             tasks.Lifecycle{MaxConcurrentPerRequestor: 1},
		GenerateID: func() (string, error) {
			return ids[nextID.Add(1)-1], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-blocked-admission", Version: "1"}, &server.Options{Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, createErr
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	startedAt := time.Now()
	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if elapsed := time.Since(startedAt); elapsed > 2500*time.Millisecond {
		t.Fatalf("admission plus rollback exceeded two bounded cleanup budgets: %v", elapsed)
	}
	if !strings.Contains(string(raw), "context deadline exceeded") {
		t.Fatalf("response = %s, want blocked admission failure", raw)
	}
	select {
	case <-started:
		t.Fatal("task handler started after admission Get timed out")
	default:
	}
	if _, err := base.Get(context.Background(), "blocked-admission"); !errors.Is(err, tasks.ErrTaskNotFound) {
		t.Fatalf("failed admission left a durable task: %v", err)
	}

	release := make(chan struct{})
	defer close(release)
	if _, err := engine.CreateForToolCall(context.Background(), tasks.CreateToolCallParams{
		ToolName: "replacement", AuthContext: "",
		Run: func(context.Context) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatalf("failed admission retained concurrency slot: %v", err)
	}
}

func TestTaskAdmissionEncodesCanonicalCreatedTask(t *testing.T) {
	store := tasks.NewInMemoryStore()
	release := make(chan struct{})
	defer close(release)
	ttl := int64(12_345)
	engine, err := tasks.NewEngine(store, &tasks.Options{
		GenerateID:   func() (string, error) { return "canonical-task", nil },
		PollInterval: 2_345,
	})
	if err != nil {
		t.Fatal(err)
	}
	var want tasks.CreatedTask
	var wantTTL, wantPoll int64
	s, err := server.New(server.Info{Name: "task-canonical", Version: "1"}, &server.Options{Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start", TaskMeta: protocolcodec.TaskMeta{TTL: &ttl},
				Run: func(context.Context) (json.RawMessage, error) {
					<-release
					return json.RawMessage(`{}`), nil
				},
			}, true)
			want = created
			wantTTL, wantPoll = *created.TTL, *created.PollInterval
			created.Status = "completed"
			created.StatusMessage = "fabricated"
			created.CreatedAt = time.Unix(1, 0).UTC()
			created.LastUpdatedAt = time.Unix(2, 0).UTC()
			*created.TTL = 1
			*created.PollInterval = 2
			created.Required = false
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, createErr
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	var response struct {
		Result struct {
			TaskID         string `json:"taskId"`
			Status         string `json:"status"`
			StatusMessage  string `json:"statusMessage"`
			CreatedAt      string `json:"createdAt"`
			LastUpdatedAt  string `json:"lastUpdatedAt"`
			TTL            int64  `json:"ttlMs"`
			PollIntervalMs int64  `json:"pollIntervalMs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	got := response.Result
	if got.TaskID != want.ID || got.Status != want.Status || got.StatusMessage != want.StatusMessage ||
		got.CreatedAt != want.CreatedAt.Format(time.RFC3339Nano) ||
		got.LastUpdatedAt != want.LastUpdatedAt.Format(time.RFC3339Nano) ||
		got.TTL != wantTTL || got.PollIntervalMs != wantPoll {
		t.Fatalf("tools/call encoded mutated task projection: got %#v, canonical %#v, raw %s", got, want, raw)
	}
}

func TestTaskAdmissionRejectsFabricatedCreatedTaskID(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, err := tasks.NewEngine(store, &tasks.Options{GenerateID: func() (string, error) { return "actual-task", nil }})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-fabricated-id", Version: "1"}, &server.Options{Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			created.ID = "fabricated-task"
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, createErr
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if !strings.Contains(string(raw), `CreatedTask \"fabricated-task\" was not created during this tools/call`) {
		t.Fatalf("response = %s, want fabricated task admission error", raw)
	}
	if strings.Contains(string(raw), `"resultType":"task"`) {
		t.Fatalf("fabricated task was emitted as a successful result: %s", raw)
	}
	assertRejectedTaskDidNotStart(t, started, store)
}

func TestTaskAdmissionDoesNotEmitDeletedCanonicalTask(t *testing.T) {
	store := tasks.NewInMemoryStore()
	started := make(chan struct{}, 1)
	engine, err := tasks.NewEngine(store, &tasks.Options{GenerateID: func() (string, error) { return "deleted-before-admit", nil }})
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Info{Name: "task-deleted-before-admit", Version: "1"}, &server.Options{Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "start"}, nil, nil,
		func(ctx context.Context, _ taskToolInput) (server.ToolOutput[taskToolOutput], error) {
			created, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
				ToolName: "start",
				Run: func(context.Context) (json.RawMessage, error) {
					started <- struct{}{}
					return json.RawMessage(`{}`), nil
				},
			}, true)
			if createErr == nil {
				createErr = store.Delete(ctx, created.ID)
			}
			return server.ToolOutput[taskToolOutput]{CreatedTask: &created}, createErr
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	raw := modernToolCall(t, ts, `{"extensions":{"io.modelcontextprotocol/tasks":{}}}`)
	if !strings.Contains(string(raw), `admit task \"deleted-before-admit\"`) {
		t.Fatalf("response = %s, want admission failure", raw)
	}
	if strings.Contains(string(raw), `"resultType":"task"`) {
		t.Fatalf("deleted task was emitted as a canonical task result: %s", raw)
	}
	select {
	case <-started:
		t.Fatal("deleted task handler started")
	default:
	}
}

func assertRejectedTaskDidNotStart(t *testing.T, started <-chan struct{}, store tasks.TaskStore) {
	t.Helper()
	select {
	case <-started:
		t.Fatal("rejected task handler started")
	case <-time.After(50 * time.Millisecond):
	}
	recs, _, err := store.List(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("rejected task left durable records: %#v", recs)
	}
}

func modernToolCall(t *testing.T, ts *httptest.Server, capabilities string) []byte {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"start","arguments":{},"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},` +
		`"io.modelcontextprotocol/clientCapabilities":` + capabilities + `}}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "start")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if strings.HasPrefix(string(raw), "event: message\n") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data: ") {
				return []byte(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	return raw
}
