package inmem_test

import (
	"context"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/inmem"
	"github.com/hurtener/dockyard/runtime/store/storetest"

	// Blank import to exercise init-block driver registration.
	_ "github.com/hurtener/dockyard/runtime/store/inmem"
)

// TestConformance runs the shared Store conformance suite against the
// in-memory driver (AGENTS.md §9 — every driver proves the seam).
func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func() store.Store { return inmem.New() })
}

// TestDriverRegistered verifies the init block registered the driver so
// store.Open("inmem", …) works.
func TestDriverRegistered(t *testing.T) {
	s, err := store.Open(context.Background(), inmem.DriverName, "")
	if err != nil {
		t.Fatalf("Open(%q): %v", inmem.DriverName, err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
