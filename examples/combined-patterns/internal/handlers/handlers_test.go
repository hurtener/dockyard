package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/dockyard/examples/combined-patterns/internal/contracts"
	"github.com/hurtener/dockyard/examples/combined-patterns/internal/handlers"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// MCP Tasks lifecycle statuses, mirrored here as constant strings so the
// test runs both in-tree and from an external module without importing
// internal/protocolcodec (Go forbids that from outside the parent
// package). The runtime types are still `protocolcodec.TaskStatus`, a
// `type TaskStatus string` — comparing the rec.Status to a literal
// string is the same equality the runtime does.
const (
	statusInputRequired = "input_required"
	statusCompleted     = "completed"
)

// TestRolloutHealth_Empty confirms a missing flag drives the empty
// state — no panic, no error, just the empty PageState.
func TestRolloutHealth_Empty(t *testing.T) {
	t.Parallel()
	s := handlers.NewSnapshot()
	got, err := s.Handler(context.Background(), contracts.RolloutHealthInput{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if got.Structured.State != "empty" {
		t.Fatalf("State = %q, want empty", got.Structured.State)
	}
	if got.Structured.Kind != "metric_card" {
		t.Fatalf("Kind = %q, want metric_card", got.Structured.Kind)
	}
}

// TestRolloutHealth_Deterministic confirms a given flag produces a
// stable health profile — the demo is reproducible.
func TestRolloutHealth_Deterministic(t *testing.T) {
	t.Parallel()
	s := handlers.NewSnapshot()
	in := contracts.RolloutHealthInput{Flag: "checkout-v2", WindowMinutes: 30}
	a, _ := s.Handler(context.Background(), in)
	b, _ := s.Handler(context.Background(), in)
	if a.Structured.Value != b.Structured.Value {
		t.Fatalf("non-deterministic value: %v vs %v", a.Structured.Value, b.Structured.Value)
	}
	if a.Structured.Tone != b.Structured.Tone {
		t.Fatalf("non-deterministic tone: %q vs %q", a.Structured.Tone, b.Structured.Tone)
	}
	if len(a.Structured.Trend) != 30 {
		t.Fatalf("Trend len = %d, want 30", len(a.Structured.Trend))
	}
}

// TestRolloutHealth_AllTones confirms the seed-based dispatch covers
// every health tone (ok / warn / critical) — the demo can showcase all
// three.
func TestRolloutHealth_AllTones(t *testing.T) {
	t.Parallel()
	s := handlers.NewSnapshot()
	seen := map[contracts.HealthTone]bool{}
	for _, flag := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		got, _ := s.Handler(context.Background(), contracts.RolloutHealthInput{Flag: flag})
		seen[got.Structured.Tone] = true
	}
	for _, tone := range []contracts.HealthTone{contracts.HealthOK, contracts.HealthWarn, contracts.HealthCritical} {
		if !seen[tone] {
			t.Errorf("tone %q never produced across the seed set", tone)
		}
	}
}

// TestProposeRolloutAction_NoEngine confirms the capability-degraded
// path: no engine, returns the awaiting prompt synchronously with the
// "host did not negotiate Tasks" message.
func TestProposeRolloutAction_NoEngine(t *testing.T) {
	t.Parallel()
	a := handlers.NewApprovalProposer(nil)
	got, err := a.Handler(context.Background(), contracts.ProposeRolloutActionInput{
		Flag:       "checkout-v2",
		Action:     contracts.ActionAdvance,
		Rationale:  "Health is OK.",
		TargetRamp: 50,
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if got.Structured.State != "awaiting" {
		t.Fatalf("State = %q, want awaiting (degraded sync render)", got.Structured.State)
	}
	if got.Structured.Title == "" {
		t.Fatalf("Title was empty")
	}
	if got.Structured.Message == "" {
		t.Fatalf("expected degraded-path Message to be populated")
	}
}

// TestProposeRolloutAction_EmptyInput confirms the empty path: no flag
// or no action → empty PageState.
func TestProposeRolloutAction_EmptyInput(t *testing.T) {
	t.Parallel()
	a := handlers.NewApprovalProposer(nil)
	got, err := a.Handler(context.Background(), contracts.ProposeRolloutActionInput{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if got.Structured.State != "empty" {
		t.Fatalf("State = %q, want empty", got.Structured.State)
	}
}

// TestProposeRolloutAction_TitleFromAction confirms each action shape
// composes the right headline. Covers actionTitle's branches without
// needing a live tasks.Engine.
func TestProposeRolloutAction_TitleFromAction(t *testing.T) {
	t.Parallel()
	a := handlers.NewApprovalProposer(nil)
	cases := []struct {
		action contracts.RolloutAction
		target int
		want   string
	}{
		{contracts.ActionAdvance, 50, "Advance flag to 50%"},
		{contracts.ActionAdvance, 0, "Advance flag"},
		{contracts.ActionPause, 0, "Pause flag"},
		{contracts.ActionRollback, 0, "Rollback flag"},
	}
	for _, tc := range cases {
		t.Run(string(tc.action), func(t *testing.T) {
			t.Parallel()
			got, err := a.Handler(context.Background(), contracts.ProposeRolloutActionInput{
				Flag:       "flag",
				Action:     tc.action,
				Rationale:  "demo",
				TargetRamp: tc.target,
			})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if got.Structured.Title != tc.want {
				t.Errorf("Title = %q, want %q", got.Structured.Title, tc.want)
			}
		})
	}
}

// drainToTerminal runs the proposal Handler against a live in-memory
// tasks.Engine, waits for input_required, supplies the given reply, and
// returns once the task transitions to a terminal status. It covers
// the goroutine-only helpers (runApproval, buildTerminal, decodeReply,
// summarise, marshalCallToolResult, decodeCreatedTaskID).
func drainToTerminal(t *testing.T, in contracts.ProposeRolloutActionInput, reply []byte, declined bool) {
	t.Helper()
	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{PollInterval: 25})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(engine.StopSweep)

	a := handlers.NewApprovalProposer(engine)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := a.Handler(ctx, in)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	taskID := got.Structured.TaskID
	if taskID == "" {
		t.Fatalf("TaskID was empty — handler did not create a task")
	}

	// Wait until the task is in input_required.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := store.Get(ctx, taskID)
		if err == nil && string(rec.Status) == statusInputRequired {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := engine.SupplyInput(ctx, taskID, tasks.InputResponse{Data: reply, Declined: declined}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}

	// Wait for a terminal status. IsTerminal() lives on protocolcodec.
	// TaskStatus but the receiver-set "terminal" set is {completed,
	// failed, cancelled} — we check the success case directly.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := store.Get(ctx, taskID)
		if err == nil {
			s := string(rec.Status)
			if s == statusCompleted {
				return
			}
			if s == "failed" || s == "cancelled" {
				t.Fatalf("status = %q, want completed", s)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task never reached terminal status")
}

// TestProposeRolloutAction_TasksEngineApproves drives an approve reply.
func TestProposeRolloutAction_TasksEngineApproves(t *testing.T) {
	t.Parallel()
	drainToTerminal(t, contracts.ProposeRolloutActionInput{
		Flag:       "checkout-v2",
		Action:     contracts.ActionAdvance,
		Rationale:  "Health is OK.",
		TargetRamp: 50,
	}, []byte(`{"approved":true,"reason":"looks good"}`), false)
}

// TestProposeRolloutAction_TasksEngineRejects drives a reject reply,
// exercising the rejected branch of buildTerminal + summarise.
func TestProposeRolloutAction_TasksEngineRejects(t *testing.T) {
	t.Parallel()
	drainToTerminal(t, contracts.ProposeRolloutActionInput{
		Flag:        "flag-x",
		Action:      contracts.ActionRollback,
		Rationale:   "errors above threshold.",
		CurrentRamp: 25,
	}, []byte(`{"approved":false,"reason":"not yet"}`), false)
}

// TestProposeRolloutAction_TasksEngineDeclines drives a declined reply,
// exercising the declined branch of buildTerminal + summarise.
func TestProposeRolloutAction_TasksEngineDeclines(t *testing.T) {
	t.Parallel()
	drainToTerminal(t, contracts.ProposeRolloutActionInput{
		Flag:      "flag-y",
		Action:    contracts.ActionPause,
		Rationale: "pause for review.",
	}, nil, true)
}
