package sqlitestore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/store/storetest"
)

// TestConformanceMemory runs the shared conformance suite against an in-memory
// SQLite database.
func TestConformanceMemory(t *testing.T) {
	storetest.RunConformance(t, func() store.Store {
		s, err := sqlitestore.Open(context.Background(), ":memory:")
		if err != nil {
			t.Fatalf("Open(:memory:): %v", err)
		}
		return s
	})
}

// TestConformanceFile runs the shared conformance suite against a file-backed
// SQLite database — each Store gets a unique path so the suite's
// open-fresh-each-time contract holds.
func TestConformanceFile(t *testing.T) {
	dir := t.TempDir()
	var n int
	storetest.RunConformance(t, func() store.Store {
		n++
		path := filepath.Join(dir, "store-"+itoa(n)+".db")
		s, err := sqlitestore.Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open(%q): %v", path, err)
		}
		return s
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestDriverRegistered verifies the init block registered the driver so
// store.Open("sqlite", …) works, including against a file path.
func TestDriverRegistered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registered.db")
	s, err := store.Open(context.Background(), sqlitestore.DriverName, path)
	if err != nil {
		t.Fatalf("Open(%q): %v", sqlitestore.DriverName, err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestOpenInvalidPath confirms Open surfaces an error for an unopenable dsn
// (a path whose parent directory does not exist).
func TestOpenInvalidPath(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "nested", "store.db")
	if _, err := sqlitestore.Open(context.Background(), bad); err == nil {
		t.Fatal("Open of an unwritable path should fail")
	}
}

// TestPersistenceAcrossReopen confirms a file-backed database keeps its data
// after Close + reopen — the durability the HTTP/Portico modes rely on.
func TestPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "persist.db")

	s1, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	if err := s1.Update(ctx, func(tx store.Tx) error {
		return tx.Put("ns", "durable", []byte("value"))
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}

	s2, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open #2: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if err := s2.View(ctx, func(tx store.Tx) error {
		got, err := tx.Get("ns", "durable")
		if err != nil {
			return err
		}
		if string(got) != "value" {
			t.Fatalf("got %q want %q after reopen", got, "value")
		}
		return nil
	}); err != nil {
		t.Fatalf("View after reopen: %v", err)
	}
}
