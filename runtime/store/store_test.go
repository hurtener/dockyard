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

func (s *fakeStore) Migrate(ctx context.Context) error { return RunMigrations(ctx, s) }

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

// --- Migration runner tests -------------------------------------------------

func TestMigrateAppliesInOrderAndIsIdempotent(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)

	var order []string
	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, tx Tx) error {
		order = append(order, "0001_a")
		return tx.Put("data", "a", []byte("1"))
	}})
	AddMigration(Migration{ID: "0002_b", Up: func(_ context.Context, tx Tx) error {
		order = append(order, "0002_b")
		return tx.Put("data", "b", []byte("2"))
	}})

	s := newFake()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if len(order) != 2 || order[0] != "0001_a" || order[1] != "0002_b" {
		t.Fatalf("migrations ran out of order: %v", order)
	}

	// Re-run: no migration should execute again.
	before := len(order)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
	if len(order) != before {
		t.Fatalf("re-run applied migrations again: %v", order)
	}

	// Schema state must be intact and identical.
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
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)

	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})

	s := newFake()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate #1: %v", err)
	}

	// Appending a new migration and re-running is allowed and applies only it.
	applied := false
	AddMigration(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error {
		applied = true
		return nil
	}})
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate #2: %v", err)
	}
	if !applied {
		t.Fatal("appended migration 0002_b did not run")
	}
}

func TestMigrateRejectsRemovedMigration(t *testing.T) {
	resetMigrationsForTest()

	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	AddMigration(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	s := newFake()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Simulate a migration being removed after merge: re-register only 0001.
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})

	err := s.Migrate(context.Background())
	if !errors.Is(err, ErrMigrationOutOfOrder) {
		t.Fatalf("removed migration: got %v want ErrMigrationOutOfOrder", err)
	}
}

func TestMigrateRejectsReordering(t *testing.T) {
	resetMigrationsForTest()

	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})
	AddMigration(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	s := newFake()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Re-register in swapped order: ordinals no longer match what was applied.
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	AddMigration(Migration{ID: "0002_b", Up: func(_ context.Context, _ Tx) error { return nil }})
	AddMigration(Migration{ID: "0001_a", Up: func(_ context.Context, _ Tx) error { return nil }})

	err := s.Migrate(context.Background())
	if !errors.Is(err, ErrMigrationOutOfOrder) {
		t.Fatalf("reordered migrations: got %v want ErrMigrationOutOfOrder", err)
	}
}

func TestMigrateFailureLeavesCleanPrefix(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)

	sentinel := errors.New("migration failed")
	AddMigration(Migration{ID: "0001_ok", Up: func(_ context.Context, tx Tx) error {
		return tx.Put("data", "ok", []byte("done"))
	}})
	AddMigration(Migration{ID: "0002_bad", Up: func(_ context.Context, _ Tx) error {
		return sentinel
	}})

	s := newFake()
	err := s.Migrate(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Migrate: got %v want the sentinel", err)
	}

	// 0001 committed; 0002 did not. A subsequent run with 0002 fixed must
	// apply only 0002.
	resetMigrationsForTest()
	rerun := false
	AddMigration(Migration{ID: "0001_ok", Up: func(_ context.Context, _ Tx) error {
		t.Fatal("0001_ok should not re-run — it already committed")
		return nil
	}})
	AddMigration(Migration{ID: "0002_bad", Up: func(_ context.Context, _ Tx) error {
		rerun = true
		return nil
	}})
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("recovery Migrate: %v", err)
	}
	if !rerun {
		t.Fatal("0002 did not re-run after its failure was fixed")
	}
}

func TestMigrateNoMigrationsIsNoop(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	s := newFake()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate with no registered migrations: %v", err)
	}
}

func TestAddMigrationDuplicatePanics(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	AddMigration(Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("duplicate AddMigration should panic")
		}
		if err, ok := r.(error); !ok || !errors.Is(err, ErrDuplicateMigration) {
			t.Fatalf("panic value %v, want ErrDuplicateMigration", r)
		}
	}()
	AddMigration(Migration{ID: "dup", Up: func(_ context.Context, _ Tx) error { return nil }})
}

func TestAddMigrationEmptyIDPanics(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	defer func() {
		if recover() == nil {
			t.Fatal("AddMigration with an empty ID should panic")
		}
	}()
	AddMigration(Migration{ID: "", Up: func(_ context.Context, _ Tx) error { return nil }})
}

func TestAddMigrationNilUpPanics(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	defer func() {
		if recover() == nil {
			t.Fatal("AddMigration with a nil Up should panic")
		}
	}()
	AddMigration(Migration{ID: "no-up", Up: nil})
}

func TestMigrateHonoursContextCancellation(t *testing.T) {
	resetMigrationsForTest()
	t.Cleanup(resetMigrationsForTest)
	AddMigration(Migration{ID: "0001", Up: func(_ context.Context, _ Tx) error { return nil }})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newFake()
	if err := s.Migrate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Migrate with a cancelled ctx: got %v want context.Canceled", err)
	}
}
