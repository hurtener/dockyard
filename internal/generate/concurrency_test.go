package generate

import (
	"bytes"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
)

// This file holds the Phase 21.5 concurrent-stress proof for generate.Plan.
// Plan documents that it is deterministic and "builds fresh state per call and
// holds no shared mutable state" — that is a reusable concurrent surface, and
// CLAUDE.md §5 requires a reusable artifact to be proven safe under concurrent
// use, §11 requires -race. Plan is the dry-run core that `dockyard validate`'s
// stale-codegen check invokes; concurrent invocation must be race-free and
// byte-deterministic.

// TestPlan_ConcurrentIsRaceFreeAndDeterministic runs generate.Plan from many
// goroutines against one scaffolded project and asserts every run produces a
// byte-identical file set with no race — Plan shares no mutable state and is
// deterministic.
func TestPlan_ConcurrentIsRaceFreeAndDeterministic(t *testing.T) {
	t.Parallel()

	projectDir := scaffoldProject(t, "gen-concurrent")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	// A reference plan from a single call — every concurrent run must match it
	// byte for byte (Plan's determinism guarantee, RFC §6.2).
	want, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("reference Plan: %v", err)
	}

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	plans := make([]map[string][]byte, goroutines)
	errs := make([]error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			plans[g], errs[g] = Plan(Options{ProjectDir: projectDir, Manifest: m})
		}(g)
	}
	wg.Wait()

	for g := 0; g < goroutines; g++ {
		if errs[g] != nil {
			t.Fatalf("goroutine %d: Plan: %v", g, errs[g])
		}
		assertSamePlan(t, g, want, plans[g])
	}
}

// assertSamePlan fails the test unless got carries exactly the same files,
// byte for byte, as want.
func assertSamePlan(t *testing.T, g int, want, got map[string][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("goroutine %d: plan has %d files, want %d", g, len(got), len(want))
	}
	wantPaths := make([]string, 0, len(want))
	for p := range want {
		wantPaths = append(wantPaths, p)
	}
	sort.Strings(wantPaths)
	for _, p := range wantPaths {
		gb, ok := got[p]
		if !ok {
			t.Fatalf("goroutine %d: plan is missing file %q", g, p)
		}
		if !bytes.Equal(gb, want[p]) {
			t.Fatalf("goroutine %d: file %q drifted under concurrency — Plan is not deterministic", g, p)
		}
	}
}
