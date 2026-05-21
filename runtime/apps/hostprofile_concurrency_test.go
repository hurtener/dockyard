package apps_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/runtime/apps"
)

// TestHostProfileRegistry_ConcurrentReuse is the reusable-artifact concurrency
// test (AGENTS.md §5, §14): the process-wide host-profile registry is a
// reusable artifact, so concurrent RegisterHostProfile / HostProfileFor /
// DerivedDomain calls must be race-free. Each worker registers a *distinct*
// profile id (so registration always succeeds) and concurrently looks up the
// built-in profiles and derives domains.
func TestHostProfileRegistry_ConcurrentReuse(t *testing.T) {
	t.Parallel()

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers*4)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Each worker registers a unique driver — no two collide.
			id := fmt.Sprintf("concurrent-driver-%d", i)
			if err := apps.RegisterHostProfile(fakeProfile{id: id}); err != nil {
				errs <- fmt.Errorf("RegisterHostProfile(%q): %w", id, err)
				return
			}

			// Concurrent lookups of the built-in profiles.
			if _, err := apps.HostProfileFor("claude"); err != nil {
				errs <- err
				return
			}
			if _, err := apps.HostProfileFor("generic"); err != nil {
				errs <- err
				return
			}

			// Concurrent derivation through the seam.
			got, err := apps.DerivedDomain("claude", "main", "https://x.example.com/mcp")
			if err != nil {
				errs <- err
				return
			}
			if !strings.HasSuffix(got, ".claudemcpcontent.com") {
				errs <- fmt.Errorf("derived %q is not a claude origin", got)
				return
			}

			// The just-registered driver is visible.
			if _, err := apps.HostProfileFor(id); err != nil {
				errs <- fmt.Errorf("HostProfileFor(%q) after register: %w", id, err)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent host-profile registry use: %v", err)
	}
}
