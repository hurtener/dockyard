// Package storetest holds the shared Store-driver conformance suite (RFC §13,
// AGENTS.md §9). Every Store driver must pass RunConformance; a new persistence
// guarantee is added here once and proven against every driver, never bolted
// onto one driver.
//
// A driver's test wires the suite in with a few lines:
//
//	func TestConformance(t *testing.T) {
//		storetest.RunConformance(t, func() store.Store { return inmem.New() })
//	}
package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
)

// conformanceCase is one named guarantee in the Store-driver conformance suite.
type conformanceCase struct {
	name string
	fn   func(*testing.T, func() store.Store)
}

// conformanceCases is the full conformance suite. It is a package-level slice
// so the harness self-guard (TestConformanceHarnessRuns) can assert the suite
// is non-empty and runs every case — a silently-broken harness must not pass
// every driver vacuously.
var conformanceCases = []conformanceCase{
	{"PutGet", testPutGet},
	{"GetMissing", testGetMissing},
	{"Overwrite", testOverwrite},
	{"Delete", testDelete},
	{"DeleteMissing", testDeleteMissing},
	{"EmptyValueRoundTrips", testEmptyValue},
	{"NamespaceIsolation", testNamespaceIsolation},
	{"ScanOrderedAndPrefixed", testScan},
	{"ScanWithWildcardPrefix", testScanWildcard},
	{"UpdateRollback", testUpdateRollback},
	{"ReadOwnWrites", testReadOwnWrites},
	{"ViewIsReadOnly", testViewReadOnly},
	{"ValueIsolation", testValueIsolation},
	{"Ping", testPing},
	{"MigrateIdempotent", testMigrateIdempotent},
	{"MigrationRunner", testMigrationRunner},
	{"ClosedStore", testClosedStore},
	{"Concurrency", testConcurrency},
}

// RunConformance exercises every guarantee of the Store seam against a driver.
// open must return a freshly-constructed, empty Store on each call; the suite
// closes each Store it opens.
func RunConformance(t *testing.T, open func() store.Store) {
	t.Helper()
	for _, tc := range conformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, open)
		})
	}
}

func ctx() context.Context { return context.Background() }

func mustUpdate(t *testing.T, s store.Store, fn func(store.Tx) error) {
	t.Helper()
	if err := s.Update(ctx(), fn); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func mustClose(t *testing.T, s store.Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func testPutGet(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error {
		return tx.Put("ns", "k", []byte("v"))
	})
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(got, []byte("v")) {
			return fmt.Errorf("got %q want %q", got, "v")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testGetMissing(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	if err := s.View(ctx(), func(tx store.Tx) error {
		_, err := tx.Get("ns", "absent")
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("got %w want ErrNotFound", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testOverwrite(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "k", []byte("first")) })
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "k", []byte("second")) })
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(got, []byte("second")) {
			return fmt.Errorf("got %q want %q", got, "second")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testDelete(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "k", []byte("v")) })
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Delete("ns", "k") })
	if err := s.View(ctx(), func(tx store.Tx) error {
		_, err := tx.Get("ns", "k")
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("got %w want ErrNotFound after Delete", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testDeleteMissing(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	// Deleting an absent key is a no-op, not an error.
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Delete("ns", "absent") })
}

func testEmptyValue(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "empty", []byte{}) })
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "empty")
		if err != nil {
			return fmt.Errorf("empty value should be present: %w", err)
		}
		if len(got) != 0 {
			return fmt.Errorf("got %q want empty", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testNamespaceIsolation(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error {
		if err := tx.Put("ns1", "k", []byte("a")); err != nil {
			return err
		}
		return tx.Put("ns2", "k", []byte("b"))
	})
	if err := s.View(ctx(), func(tx store.Tx) error {
		v1, err := tx.Get("ns1", "k")
		if err != nil {
			return err
		}
		v2, err := tx.Get("ns2", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(v1, []byte("a")) || !bytes.Equal(v2, []byte("b")) {
			return fmt.Errorf("namespaces leaked: ns1=%q ns2=%q", v1, v2)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testScan(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error {
		for _, k := range []string{"task:c", "task:a", "task:b", "obs:x"} {
			if err := tx.Put("ns", k, []byte(k)); err != nil {
				return err
			}
		}
		return nil
	})
	if err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan("ns", "task:")
		if err != nil {
			return err
		}
		want := []string{"task:a", "task:b", "task:c"}
		if len(kvs) != len(want) {
			return fmt.Errorf("got %d entries want %d", len(kvs), len(want))
		}
		for i, kv := range kvs {
			if kv.Key != want[i] {
				return fmt.Errorf("entry %d: got %q want %q (scan not ordered)", i, kv.Key, want[i])
			}
			if !bytes.Equal(kv.Value, []byte(want[i])) {
				return fmt.Errorf("entry %d: value %q want %q", i, kv.Value, want[i])
			}
		}
		// An empty prefix scans the whole namespace.
		all, err := tx.Scan("ns", "")
		if err != nil {
			return err
		}
		if len(all) != 4 {
			return fmt.Errorf("empty-prefix scan got %d want 4", len(all))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testScanWildcard(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	// Keys containing SQL LIKE wildcards must be matched literally.
	mustUpdate(t, s, func(tx store.Tx) error {
		for _, k := range []string{"a%b", "a_b", "axb", "azb"} {
			if err := tx.Put("ns", k, []byte(k)); err != nil {
				return err
			}
		}
		return nil
	})
	if err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan("ns", "a%")
		if err != nil {
			return err
		}
		if len(kvs) != 1 || kvs[0].Key != "a%b" {
			return fmt.Errorf("prefix %q matched %v — wildcard not escaped", "a%", keys(kvs))
		}
		kvs, err = tx.Scan("ns", "a_")
		if err != nil {
			return err
		}
		if len(kvs) != 1 || kvs[0].Key != "a_b" {
			return fmt.Errorf("prefix %q matched %v — wildcard not escaped", "a_", keys(kvs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func keys(kvs []store.KeyValue) []string {
	out := make([]string, len(kvs))
	for i, kv := range kvs {
		out[i] = kv.Key
	}
	return out
}

func testUpdateRollback(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "k", []byte("committed")) })

	sentinel := errors.New("intentional failure")
	err := s.Update(ctx(), func(tx store.Tx) error {
		if err := tx.Put("ns", "k", []byte("rolled-back")); err != nil {
			return err
		}
		if err := tx.Put("ns", "new", []byte("rolled-back")); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update returned %v, want the sentinel", err)
	}
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(got, []byte("committed")) {
			return fmt.Errorf("k = %q, rollback did not restore it", got)
		}
		if _, err := tx.Get("ns", "new"); !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("'new' key survived a rolled-back transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testReadOwnWrites(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	if err := s.Update(ctx(), func(tx store.Tx) error {
		if err := tx.Put("ns", "k", []byte("v")); err != nil {
			return err
		}
		got, err := tx.Get("ns", "k")
		if err != nil {
			return fmt.Errorf("transaction cannot read its own write: %w", err)
		}
		if !bytes.Equal(got, []byte("v")) {
			return fmt.Errorf("read-own-write got %q want %q", got, "v")
		}
		// A scan within the transaction must also see the staged write.
		kvs, err := tx.Scan("ns", "")
		if err != nil {
			return err
		}
		if len(kvs) != 1 {
			return fmt.Errorf("read-own-write scan got %d want 1", len(kvs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testViewReadOnly(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	// A write attempt inside View must fail with ErrReadOnly; the keyspace
	// must stay empty.
	err := s.View(ctx(), func(tx store.Tx) error {
		return tx.Put("ns", "k", []byte("v"))
	})
	if !errors.Is(err, store.ErrReadOnly) {
		t.Fatalf("Put inside View: got %v want ErrReadOnly", err)
	}
	// Delete inside View must also fail with ErrReadOnly.
	if err := s.View(ctx(), func(tx store.Tx) error {
		return tx.Delete("ns", "k")
	}); !errors.Is(err, store.ErrReadOnly) {
		t.Fatalf("Delete inside View: got %v want ErrReadOnly", err)
	}
	if err := s.View(ctx(), func(tx store.Tx) error {
		if _, err := tx.Get("ns", "k"); !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("a write leaked through View")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testValueIsolation(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	original := []byte("original")
	mustUpdate(t, s, func(tx store.Tx) error { return tx.Put("ns", "k", original) })
	// Mutating the caller's slice after Put must not change stored data.
	original[0] = 'X'
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(got, []byte("original")) {
			return fmt.Errorf("stored value mutated by caller: %q", got)
		}
		// Mutating the returned slice must not change stored data either.
		got[0] = 'Y'
		got2, err := tx.Get("ns", "k")
		if err != nil {
			return err
		}
		if !bytes.Equal(got2, []byte("original")) {
			return fmt.Errorf("stored value mutated via returned slice: %q", got2)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testPing(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	if err := s.Ping(ctx()); err != nil {
		t.Fatalf("Ping on a fresh store: %v", err)
	}
}

func testMigrateIdempotent(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)
	// A clean Migrate and a re-run Migrate with a nil set must both succeed —
	// Migrate with no migrations is a valid no-op, safe to call repeatedly.
	if err := s.Migrate(ctx(), nil); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := s.Migrate(ctx(), nil); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
	if err := s.Migrate(ctx(), nil); err != nil {
		t.Fatalf("third Migrate: %v", err)
	}
}

// migrationsNamespace is the reserved KV namespace store.RunMigrations records
// applied migrations in. It mirrors the unexported store.migrationNamespace
// constant; the suite asserts the record lands here.
const migrationsNamespace = "__store_migrations__"

// testMigrationRunner exercises the real migration runner end-to-end against
// the driver under test — not an in-package fake. It builds a real migration
// in a caller-owned store.MigrationSet, applies it via Store.Migrate, asserts
// it ran exactly once, is idempotent on re-run, and is recorded in the
// __store_migrations__ namespace. It runs against every driver, so the runner
// is proven on inmem AND sqlitestore (CLAUDE.md §9, §17). The MigrationSet is
// local to this test — there is no process-global registry to isolate (D-073).
func testMigrationRunner(t *testing.T, open func() store.Store) {
	const migrationID = "0001_storetest_seed"
	var runCount int
	set := store.NewMigrationSet().MustAdd(store.Migration{
		ID: migrationID,
		Up: func(_ context.Context, tx store.Tx) error {
			runCount++
			return tx.Put("storetest_migrated", "marker", []byte("applied"))
		},
	})

	s := open()
	defer mustClose(t, s)

	// First Migrate: the migration must apply exactly once.
	if err := s.Migrate(ctx(), set); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("migration Up ran %d times on first Migrate, want 1", runCount)
	}

	// The migration's effect must be visible.
	if err := s.View(ctx(), func(tx store.Tx) error {
		got, err := tx.Get("storetest_migrated", "marker")
		if err != nil {
			return fmt.Errorf("migration effect missing: %w", err)
		}
		if !bytes.Equal(got, []byte("applied")) {
			return fmt.Errorf("migration marker = %q, want %q", got, "applied")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The runner must have recorded the migration in its reserved namespace.
	if err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan(migrationsNamespace, "")
		if err != nil {
			return err
		}
		if len(kvs) != 1 {
			return fmt.Errorf("%s has %d records, want 1", migrationsNamespace, len(kvs))
		}
		if kvs[0].Key != migrationID {
			return fmt.Errorf("recorded migration key %q, want %q", kvs[0].Key, migrationID)
		}
		if len(kvs[0].Value) == 0 {
			return fmt.Errorf("migration record for %q has an empty value", migrationID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Re-run: idempotent — the migration must not run again.
	if err := s.Migrate(ctx(), set); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("migration Up ran %d times after re-run, want 1 (not idempotent)", runCount)
	}
	if err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan(migrationsNamespace, "")
		if err != nil {
			return err
		}
		if len(kvs) != 1 {
			return fmt.Errorf("re-run left %d migration records, want 1", len(kvs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testClosedStore(t *testing.T, open func() store.Store) {
	s := open()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close is idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close should be a no-op: %v", err)
	}
	if err := s.Ping(ctx()); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("Ping after Close: got %v want ErrClosed", err)
	}
	if err := s.View(ctx(), func(store.Tx) error { return nil }); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("View after Close: got %v want ErrClosed", err)
	}
	if err := s.Update(ctx(), func(store.Tx) error { return nil }); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("Update after Close: got %v want ErrClosed", err)
	}
}

// testConcurrency proves a single Store is safe for concurrent use — the
// reusable-artifact guarantee (AGENTS.md §5, §14). It is meaningful only under
// the race detector.
func testConcurrency(t *testing.T, open func() store.Store) {
	s := open()
	defer mustClose(t, s)

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup
	errCh := make(chan error, workers*3)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("w%02d-k%03d", w, i)
				val := []byte(fmt.Sprintf("v-%d-%d", w, i))
				if err := s.Update(ctx(), func(tx store.Tx) error {
					return tx.Put("concurrent", key, val)
				}); err != nil {
					errCh <- fmt.Errorf("worker %d Update: %w", w, err)
					return
				}
				if err := s.View(ctx(), func(tx store.Tx) error {
					got, err := tx.Get("concurrent", key)
					if err != nil {
						return err
					}
					if !bytes.Equal(got, val) {
						return fmt.Errorf("worker %d read %q want %q", w, got, val)
					}
					return nil
				}); err != nil {
					errCh <- err
					return
				}
				if _, err := scanInView(s); err != nil {
					errCh <- fmt.Errorf("worker %d Scan: %w", w, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	// Every write must be visible after the goroutines join.
	if err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan("concurrent", "")
		if err != nil {
			return err
		}
		if want := workers * perWorker; len(kvs) != want {
			return fmt.Errorf("after concurrent writes got %d entries want %d", len(kvs), want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func scanInView(s store.Store) (int, error) {
	var n int
	err := s.View(ctx(), func(tx store.Tx) error {
		kvs, err := tx.Scan("concurrent", "")
		n = len(kvs)
		return err
	})
	return n, err
}
