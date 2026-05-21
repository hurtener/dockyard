// Package integration holds Dockyard's cross-subsystem integration tests
// (AGENTS.md §17). This file exercises the Store seam (RFC §13) with its two
// real V1 drivers — inmem and sqlitestore — through the public store.Open
// factory: no mocks at the seam boundary.
package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"

	// Blank imports register the two V1 drivers via their init blocks — the
	// same wiring a Dockyard app uses.
	_ "github.com/hurtener/dockyard/runtime/store/inmem"
	_ "github.com/hurtener/dockyard/runtime/store/sqlitestore"
)

// TestStoreSeamBothDrivers opens each registered V1 driver through store.Open,
// migrates it, performs a cross-transaction read/write round-trip, and
// exercises a rollback failure mode — proving the seam is wired end-to-end and
// behaves identically across drivers.
func TestStoreSeamBothDrivers(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		driver string
		dsn    func() string
	}{
		{"inmem", func() string { return "" }},
		{"sqlite", func() string { return filepath.Join(t.TempDir(), "seam.db") }},
	}

	for _, tc := range cases {
		t.Run(tc.driver, func(t *testing.T) {
			s, err := store.Open(ctx, tc.driver, tc.dsn())
			if err != nil {
				t.Fatalf("store.Open(%q): %v", tc.driver, err)
			}
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			})

			if err := s.Migrate(ctx, nil); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if err := s.Ping(ctx); err != nil {
				t.Fatalf("Ping: %v", err)
			}

			// Write in one transaction, read back in another.
			if err := s.Update(ctx, func(tx store.Tx) error {
				return tx.Put("seam", "k", []byte("seam-value"))
			}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if err := s.View(ctx, func(tx store.Tx) error {
				got, err := tx.Get("seam", "k")
				if err != nil {
					return err
				}
				if string(got) != "seam-value" {
					t.Fatalf("got %q want %q", got, "seam-value")
				}
				return nil
			}); err != nil {
				t.Fatalf("View: %v", err)
			}

			// Failure mode: a rolled-back Update must not persist.
			boom := errors.New("intentional rollback")
			if err := s.Update(ctx, func(tx store.Tx) error {
				if err := tx.Put("seam", "k", []byte("should-not-stick")); err != nil {
					return err
				}
				return boom
			}); !errors.Is(err, boom) {
				t.Fatalf("Update rollback: got %v want the sentinel", err)
			}
			if err := s.View(ctx, func(tx store.Tx) error {
				got, err := tx.Get("seam", "k")
				if err != nil {
					return err
				}
				if string(got) != "seam-value" {
					t.Fatalf("rollback leaked: k = %q", got)
				}
				return nil
			}); err != nil {
				t.Fatalf("View after rollback: %v", err)
			}
		})
	}
}

// TestStoreSeamUnknownDriver confirms the factory rejects an unregistered
// driver name with the typed sentinel.
func TestStoreSeamUnknownDriver(t *testing.T) {
	_, err := store.Open(context.Background(), "postgres", "")
	if !errors.Is(err, store.ErrUnknownDriver) {
		t.Fatalf("Open unknown driver: got %v want ErrUnknownDriver", err)
	}
}
