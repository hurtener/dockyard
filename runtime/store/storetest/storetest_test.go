package storetest

import (
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/inmem"
)

// TestConformanceHarnessRuns is the harness self-guard: a meta-assertion that
// the conformance suite actually executes its N subtests. Without it a
// silently-broken harness — an empty case list, or a RunConformance that loops
// over nothing — would pass every Store driver vacuously. It mirrors
// protocolcodec's TestSeamGuardActuallyScans (AGENTS.md §17.5/§17.7).
func TestConformanceHarnessRuns(t *testing.T) {
	if len(conformanceCases) == 0 {
		t.Fatal("conformance suite has zero cases — the harness is broken")
	}

	// Drive the suite against the in-memory driver and count cases that
	// actually run. Every case must execute (and pass).
	var ran int
	for _, tc := range conformanceCases {
		ran++
		ok := t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, func() store.Store { return inmem.New() })
		})
		if !ok {
			t.Fatalf("conformance case %q failed under the harness self-guard", tc.name)
		}
	}
	if ran != len(conformanceCases) {
		t.Fatalf("harness ran %d cases, want %d", ran, len(conformanceCases))
	}
	if ran < 10 {
		t.Fatalf("conformance suite ran only %d cases — implausibly small, harness likely broken", ran)
	}
}

// TestRunConformanceInMemory wires the in-memory driver through the public
// RunConformance entry point, so the suite's own entry point is exercised.
func TestRunConformanceInMemory(t *testing.T) {
	RunConformance(t, func() store.Store { return inmem.New() })
}

// TestRunBenchmarksSmoke exercises the Phase 21.5 shared Store benchmark suite
// (RunBenchmarks) as an ordinary test. `make bench` runs the benchmarks for
// real numbers; this self-guard drives them inside `go test` so the benchmark
// code is covered and a silently-broken benchmark — a panicking seed, a
// misnamed namespace — is caught by the normal suite, not only by `make bench`.
// It mirrors the conformance harness self-guard above.
func TestRunBenchmarksSmoke(t *testing.T) {
	res := testing.Benchmark(func(b *testing.B) {
		RunBenchmarks(b, func() store.Store { return inmem.New() })
	})
	if res.N < 1 {
		t.Fatalf("RunBenchmarks did not execute any iteration: %+v", res)
	}
}
