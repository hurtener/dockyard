package validate

import (
	"sync"
	"testing"
)

// This file holds the Phase 21.5 concurrent-stress proof for validate.Run. Run
// documents that it "builds fresh state per call and holds no shared mutable
// state" — that is a reusable concurrent surface, and CLAUDE.md §5 requires a
// reusable artifact to be proven safe under concurrent use, §11 requires -race.
// `dockyard test` and a future inspector can legitimately validate several
// projects (or one project repeatedly) concurrently; this test proves Run is
// safe under that load.

// TestRun_ConcurrentIsRaceFree runs validate.Run from many goroutines against
// one scaffolded project and asserts every run produces an identical Report
// with no race — Run shares no mutable state across calls.
func TestRun_ConcurrentIsRaceFree(t *testing.T) {
	t.Parallel()

	projectDir := scaffoldAndGenerate(t, "val-concurrent")

	// A reference report from a single call — every concurrent run must match.
	want, err := Run(Options{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("reference Run: %v", err)
	}

	const goroutines = 12
	var wg sync.WaitGroup
	wg.Add(goroutines)
	reports := make([]*Report, goroutines)
	errs := make([]error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			reports[g], errs[g] = Run(Options{ProjectDir: projectDir})
		}(g)
	}
	wg.Wait()

	for g := 0; g < goroutines; g++ {
		if errs[g] != nil {
			t.Fatalf("goroutine %d: Run: %v", g, errs[g])
		}
		got := reports[g]
		if got == nil {
			t.Fatalf("goroutine %d: nil report", g)
		}
		if len(got.Diagnostics) != len(want.Diagnostics) {
			t.Fatalf("goroutine %d: got %d diagnostics, want %d — a concurrent run drifted",
				g, len(got.Diagnostics), len(want.Diagnostics))
		}
		if got.HasBlockers() != want.HasBlockers() {
			t.Fatalf("goroutine %d: HasBlockers = %v, want %v",
				g, got.HasBlockers(), want.HasBlockers())
		}
		for i := range got.Diagnostics {
			if got.Diagnostics[i].String() != want.Diagnostics[i].String() {
				t.Fatalf("goroutine %d: diagnostic %d drifted: got %q want %q",
					g, i, got.Diagnostics[i].String(), want.Diagnostics[i].String())
			}
		}
	}
}
