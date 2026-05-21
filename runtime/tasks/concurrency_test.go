package tasks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// TestEngineConcurrentReuse proves the Engine is safe for concurrent use — the
// "reusable artifact ⇒ concurrent-reuse test under -race" rule (AGENTS.md §14).
// Many goroutines create tasks and dispatch tasks/* against the one engine.
func TestEngineConcurrentReuse(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{Logger: quietLogger(), AdvertiseList: true, RequestorIdentifiable: true})
	codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)

	const workers = 24
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			ctx := context.Background()
			raw, err := e.CreateForToolCall(ctx, CreateToolCallParams{
				ToolName: "concurrent",
				Run:      instantRun(json.RawMessage(`{"isError":false}`), nil),
			})
			if err != nil {
				t.Errorf("CreateForToolCall: %v", err)
				return
			}
			res, err := codec.DecodeCreateTaskResult(raw)
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			id := res.Task.ID

			// Concurrent tasks/get on the same engine.
			p, _ := codec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})
			if _, err := e.Dispatch(ctx, MethodGet, p); err != nil {
				t.Errorf("tasks/get: %v", err)
			}
			// tasks/result blocks until terminal — the task completes instantly.
			if _, err := e.Dispatch(ctx, MethodResult, p); err != nil {
				t.Errorf("tasks/result: %v", err)
			}
			// tasks/list races other goroutines' creates.
			if _, err := e.Dispatch(ctx, MethodList, nil); err != nil {
				t.Errorf("tasks/list: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestEngineConcurrentResultWaiters proves many goroutines may block on
// tasks/result for the same task and all wake when it finishes.
func TestEngineConcurrentResultWaiters(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{Logger: quietLogger()})
	codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)
	release := make(chan struct{})
	id := taskIDOf(t, e, blockingRun(release, json.RawMessage(`{"isError":false}`), nil))
	p, _ := codec.EncodeTaskIDParams(protocolcodec.TaskID{ID: id})

	const waiters = 16
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			if _, err := e.Dispatch(context.Background(), MethodResult, p); err != nil {
				t.Errorf("tasks/result: %v", err)
			}
		}()
	}
	close(release)
	wg.Wait()
}
