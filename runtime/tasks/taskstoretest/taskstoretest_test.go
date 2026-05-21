package taskstoretest

import (
	"testing"

	"github.com/hurtener/dockyard/runtime/tasks"
)

// TestConformanceHarnessRuns is the harness self-guard: a meta-assertion that
// the TaskStore conformance suite actually executes its cases. Without it a
// silently-broken harness — an empty case list — would pass every driver
// vacuously (CLAUDE.md §17; mirrors storetest.TestConformanceHarnessRuns).
func TestConformanceHarnessRuns(t *testing.T) {
	if Cases() == 0 {
		t.Fatal("TaskStore conformance suite has zero cases — the harness is broken")
	}
	var ran int
	for _, tc := range conformanceCases {
		ran++
		ok := t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, func() tasks.TaskStore { return tasks.NewInMemoryStore() })
		})
		if !ok {
			t.Fatalf("conformance case %q failed under the harness self-guard", tc.name)
		}
	}
	if ran != Cases() {
		t.Fatalf("harness ran %d cases, want %d", ran, Cases())
	}
	if ran < 10 {
		t.Fatalf("conformance suite ran only %d cases — implausibly small", ran)
	}
}

// TestInMemoryStubConformance runs the suite against the Phase 13 in-memory
// stub driver through the public RunConformance entry point.
func TestInMemoryStubConformance(t *testing.T) {
	RunConformance(t, func() tasks.TaskStore { return tasks.NewInMemoryStore() })
}
