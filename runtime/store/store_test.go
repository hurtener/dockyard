package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
)

// fakeStore is a minimal in-package Store used to drive the migration runner
// and registry tests without importing a real driver (which would create an
// import cycle). It implements the KV seam with a guarded map.
type fakeStore struct {
	mu     sync.Mutex
	data   map[string]map[string][]byte
	closed bool
}

func newFake() *fakeStore {
	return &fakeStore{data: map[string]map[string][]byte{}}
}

func (s *fakeStore) Migrate(ctx context.Context, set *MigrationSet) error {
	return RunMigrations(ctx, s, set)
}

func (s *fakeStore) View(_ context.Context, fn func(Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return fn(&fakeTx{store: s, writable: false})
}

func (s *fakeStore) Update(_ context.Context, fn func(Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return fn(&fakeTx{store: s, writable: true})
}

func (s *fakeStore) Ping(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return nil
}

func (s *fakeStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

type fakeTx struct {
	store    *fakeStore
	writable bool
}

func (t *fakeTx) Get(ns, key string) ([]byte, error) {
	if m := t.store.data[ns]; m != nil {
		if v, ok := m[key]; ok {
			return append([]byte(nil), v...), nil
		}
	}
	return nil, ErrNotFound
}

func (t *fakeTx) Put(ns, key string, value []byte) error {
	if !t.writable {
		return ErrClosed
	}
	if t.store.data[ns] == nil {
		t.store.data[ns] = map[string][]byte{}
	}
	t.store.data[ns][key] = append([]byte(nil), value...)
	return nil
}

func (t *fakeTx) Delete(ns, key string) error {
	if !t.writable {
		return ErrClosed
	}
	if m := t.store.data[ns]; m != nil {
		delete(m, key)
	}
	return nil
}

func (t *fakeTx) Scan(ns, prefix string) ([]KeyValue, error) {
	var out []KeyValue
	for k, v := range t.store.data[ns] {
		if len(prefix) <= len(k) && k[:len(prefix)] == prefix {
			out = append(out, KeyValue{Key: k, Value: append([]byte(nil), v...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// --- Registry tests ---------------------------------------------------------

func TestOpenUnknownDriver(t *testing.T) {
	_, err := Open(context.Background(), "no-such-driver", "")
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("Open unknown driver: got %v want ErrUnknownDriver", err)
	}
}

func TestRegisterAndOpen(t *testing.T) {
	name := "store-test-driver"
	Register(name, func(context.Context, string) (Store, error) { return newFake(), nil })

	found := false
	for _, d := range Drivers() {
		if d == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Drivers() did not list %q: %v", name, Drivers())
	}

	s, err := Open(context.Background(), name, "")
	if err != nil {
		t.Fatalf("Open(%q): %v", name, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := "store-test-dup-driver"
	Register(name, func(context.Context, string) (Store, error) { return newFake(), nil })
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("duplicate Register should panic")
		}
		if err, ok := r.(error); !ok || !errors.Is(err, ErrDuplicateDriver) {
			t.Fatalf("panic value %v, want ErrDuplicateDriver", r)
		}
	}()
	Register(name, func(context.Context, string) (Store, error) { return newFake(), nil })
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Register with a nil factory should panic")
		}
	}()
	Register("store-test-nil", nil)
}

func TestOpenFactoryError(t *testing.T) {
	name := "store-test-failing-driver"
	sentinel := errors.New("factory boom")
	Register(name, func(context.Context, string) (Store, error) { return nil, sentinel })
	_, err := Open(context.Background(), name, "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Open: got %v want the factory error", err)
	}
}

// --- Migration runner + MigrationSet tests ----------------------------------
//
// The migration registry is a caller-owned [MigrationSet] value, not a process
// global (D-073). Every test below builds its own set; there is no shared
// state, so every one is t.Parallel()-safe — which is itself the S1 fix the
// Wave 5 checkpoint filed (a t.Parallel()-unsafe global registry). The
// concurrency test TestMigrationSet_ConcurrentMigrate proves it under -race.

func TestMigrateAppliesInOrderAndIsIdempotent(t *testing.T) {
	t.Parallel()
	var order []string
	set, err := NewMigrationSet().
		Add(Migration{ID: "0001_a", Up: func(_ context.Context, tx Tx) error {
			order = append(order, "0001_a")
			return tx.Put("data", "a", []byte("1"))
		}})
	if err != nil {
		t.Fatalf("Add 0001_a: %v", err)
	}
	if _, err := set.Add(Migration{ID: "0002_b", Up: func(_ context.Context, tx Tx) error {
		order = append(order, "0002_b")
		return tx.Put("data", "b", []byte("2"))
	}}); err != nil {
		t.Fatalf("Add 0002_b: %v", err)
	}

	s := newFake()
	if err := s.Migrate(context.Background(), set); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if len(order) != 2 || order[0] != "0001_a" || order[1] != "0002_b" {
		t.Fatalf("migrations ran out of order: %v", order)
	}

	before := len(order)
	if err := s.Migrate(context.Background(), set); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
	if len(order) != before {
		t.Fatalf("re-run applied migrations again: %v", order)
	}

	if err := s.View(context.Background(), func(tx Tx) error {
		a, err := tx.Get("data", "a")
		if err != nil {
			return err
		}
		b, err := tx.Get("data", "b")
		if err != nil {
			return err
		}
		if string(a) != "1" || string(b) != "2" {
			t.Fatalf("post-migration state wrong: a=%q b=%q", a, b)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateAppendOnlyExtension(t *testing.T) {
	t.Parallel()
	set := NewMigrationSet().MustAdd(
		Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})

	s := newFake()
	if err := s.Migrate(context.Background(), set); err != nil {
		t.Fatalf("Migrate #1: %v", err)
	}

	// Appending a new migration and re-running applies only the new one.
	applied := false
	set.MustAdd(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error {
		applied = true
		return nil
	}})
	if err := s.Migrate(context.Background(), set); err != nil {
		t.Fatalf("Migrate #2: %v", err)
	}
	if !applied {
		t.Fatal("appended migration 0002_b did not run")
	}
}

func TestMigrateRejectsRemovedMigration(t *testing.T) {
	t.Parallel()
	full := NewMigrationSet().
		MustAdd(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }}).
		MustAdd(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	s := newFake()
	if err := s.Migrate(context.Background(), full); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A set missing a previously-applied migration is rejected.
	shrunk := NewMigrationSet().
		MustAdd(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	if err := s.Migrate(context.Background(), shrunk); !errors.Is(err, ErrMigrationOutOfOrder) {
		t.Fatalf("removed migration: got %v want ErrMigrationOutOfOrder", err)
	}
}

func TestMigrateRejectsReordering(t *testing.T) {
	t.Parallel()
	ordered := NewMigrationSet().
		MustAdd(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }}).
		MustAdd(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	s := newFake()
	if err := s.Migrate(context.Background(), ordered); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A set with swapped order: ordinals no longer match what was applied.
	swapped := NewMigrationSet().
		MustAdd(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }}).
		MustAdd(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	if err := s.Migrate(context.Background(), swapped); !errors.Is(err, ErrMigrationOutOfOrder) {
		t.Fatalf("reordered migrations: got %v want ErrMigrationOutOfOrder", err)
	}
}

func TestMigrateFailureLeavesCleanPrefix(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("migration failed")
	bad := NewMigrationSet().
		MustAdd(Migration{ID: "0001_ok", Up: func(_ context.Context, tx Tx) error {
			return tx.Put("data", "ok", []byte("done"))
		}}).
		MustAdd(Migration{ID: "0002_bad", Up: func(_ context.Context, _ Tx) error {
			return sentinel
		}})

	s := newFake()
	if err := s.Migrate(context.Background(), bad); !errors.Is(err, sentinel) {
		t.Fatalf("Migrate: got %v want the sentinel", err)
	}

	// 0001 committed; 0002 did not. A recovery set with 0002 fixed applies
	// only 0002.
	rerun := false
	fixed := NewMigrationSet().
		MustAdd(Migration{ID: "0001_ok", Up: func(_ context.Context, _ Tx) error {
			t.Fatal("0001_ok should not re-run — it already committed")
			return nil
		}}).
		MustAdd(Migration{ID: "0002_bad", Up: func(_ context.Context, _ Tx) error {
			rerun = true
			return nil
		}})
	if err := s.Migrate(context.Background(), fixed); err != nil {
		t.Fatalf("recovery Migrate: %v", err)
	}
	if !rerun {
		t.Fatal("0002 did not re-run after its failure was fixed")
	}
}

func TestMigrateNilSetIsNoop(t *testing.T) {
	t.Parallel()
	s := newFake()
	if err := s.Migrate(context.Background(), nil); err != nil {
		t.Fatalf("Migrate with a nil set: %v", err)
	}
	if err := s.Migrate(context.Background(), NewMigrationSet()); err != nil {
		t.Fatalf("Migrate with an empty set: %v", err)
	}
}

func TestMigrationSetAddDuplicateReturnsError(t *testing.T) {
	t.Parallel()
	set := NewMigrationSet().MustAdd(
		Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
	_, err := set.Add(Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
	if !errors.Is(err, ErrDuplicateMigration) {
		t.Fatalf("duplicate Add: got %v want ErrDuplicateMigration", err)
	}
	if set.Len() != 1 {
		t.Fatalf("rejected duplicate still mutated the set: Len=%d want 1", set.Len())
	}
}

func TestMigrationSetAddEmptyIDAndNilUp(t *testing.T) {
	t.Parallel()
	set := NewMigrationSet()
	if _, err := set.Add(Migration{ID: "", Up: func(_ context.Context, _ Tx) error { return nil }}); err == nil {
		t.Fatal("Add with an empty ID should return an error")
	}
	if _, err := set.Add(Migration{ID: "no-up", Up: nil}); err == nil {
		t.Fatal("Add with a nil Up should return an error")
	}
	if set.Len() != 0 {
		t.Fatalf("malformed Add mutated the set: Len=%d want 0", set.Len())
	}
}

func TestMigrationSetMustAddPanicsOnDuplicate(t *testing.T) {
	t.Parallel()
	set := NewMigrationSet().MustAdd(
		Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustAdd of a duplicate should panic")
		}
		if err, ok := r.(error); !ok || !errors.Is(err, ErrDuplicateMigration) {
			t.Fatalf("panic value %v, want ErrDuplicateMigration", r)
		}
	}()
	set.MustAdd(Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
}

func TestMigrationSetExtend(t *testing.T) {
	t.Parallel()
	a := NewMigrationSet().MustAdd(
		Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	b := NewMigrationSet().MustAdd(
		Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	if _, err := a.Extend(b); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if a.Len() != 2 {
		t.Fatalf("after Extend Len=%d want 2", a.Len())
	}
	// A clashing ID across sets is rejected.
	c := NewMigrationSet().MustAdd(
		Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	if _, err := a.Extend(c); !errors.Is(err, ErrDuplicateMigration) {
		t.Fatalf("Extend with a clashing ID: got %v want ErrDuplicateMigration", err)
	}
	// Extending with nil is a no-op.
	if _, err := a.Extend(nil); err != nil {
		t.Fatalf("Extend(nil): %v", err)
	}
}

func TestMigrateHonoursContextCancellation(t *testing.T) {
	t.Parallel()
	set := NewMigrationSet().MustAdd(
		Migration{ID: "0001", Up: func(_ context.Context, _ Tx) error { return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newFake()
	if err := s.Migrate(ctx, set); !errors.Is(err, context.Canceled) {
		t.Fatalf("Migrate with a cancelled ctx: got %v want context.Canceled", err)
	}
}

// TestMigrationSet_ConcurrentMigrate is the S1 fix proof (D-073, Wave 5
// checkpoint follow-up). The former process-global migration registry forced
// test fixtures to serialize the reset→register→Migrate sequence with an
// external mutex; concurrent fixtures otherwise raced the global and the
// duplicate-ID panic fired on timing luck alone.
//
// With the registry replaced by a caller-owned MigrationSet there is no shared
// state: N goroutines each build their own set and migrate their own store
// with zero coordination. Run under -race, this proves the fix — no race, no
// panic, every store correctly migrated.
func TestMigrationSet_ConcurrentMigrate(t *testing.T) {
	t.Parallel()
	const goroutines = 32
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each goroutine builds its OWN set — no global, no lock.
			set := NewMigrationSet().MustAdd(Migration{
				ID: "0001_concurrent",
				Up: func(_ context.Context, tx Tx) error {
					return tx.Put("concurrent", "seed", []byte("ok"))
				},
			})
			s := newFake()
			if err := s.Migrate(context.Background(), set); err != nil {
				errs[idx] = err
				return
			}
			// Re-migrate the same store from the same set: idempotent.
			if err := s.Migrate(context.Background(), set); err != nil {
				errs[idx] = err
				return
			}
			errs[idx] = s.View(context.Background(), func(tx Tx) error {
				v, err := tx.Get("concurrent", "seed")
				if err != nil {
					return err
				}
				if string(v) != "ok" {
					return errors.New("migration effect missing")
				}
				return nil
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}
