package tasks_test

import (
	"context"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/inmem"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tasks/taskstoretest"
)

// openDurable builds a fresh durable TaskStore over a fresh backing Store. The
// durable TaskStore's forward-only migration is supplied as a caller-owned
// store.MigrationSet ([tasks.Migrations]) — there is no process-global
// registry to isolate, so this fixture is t.Parallel()-safe by construction
// (D-073, the S1 fix).
func openDurable(t *testing.T, mk func() store.Store) taskstoretest.OpenFunc {
	t.Helper()
	return func() tasks.TaskStore {
		st := mk()
		if err := st.Migrate(context.Background(), tasks.Migrations()); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		ts, err := tasks.NewStore(st)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return ts
	}
}

// TestDurableTaskStore_OverInmemStore runs the shared TaskStore conformance
// suite against the durable facade layered over the in-memory Store driver.
func TestDurableTaskStore_OverInmemStore(t *testing.T) {
	t.Parallel()
	taskstoretest.RunConformance(t, openDurable(t, func() store.Store { return inmem.New() }))
}

// TestDurableTaskStore_OverSQLiteStore runs the shared TaskStore conformance
// suite against the durable facade layered over the modernc.org/sqlite Store
// driver — the V1 durable backing. modernc.org/sqlite is pure-Go; no CGo
// dependency is introduced (brief 06 §2.8, D-026).
func TestDurableTaskStore_OverSQLiteStore(t *testing.T) {
	t.Parallel()
	taskstoretest.RunConformance(t, openDurable(t, func() store.Store {
		st, err := sqlitestore.Open(context.Background(), ":memory:")
		if err != nil {
			t.Fatalf("sqlitestore.Open: %v", err)
		}
		return st
	}))
}

// TestNewStore_RejectsNilStore proves the durable driver constructor rejects a
// nil backing Store rather than panicking later.
func TestNewStore_RejectsNilStore(t *testing.T) {
	t.Parallel()
	if _, err := tasks.NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) must return an error")
	}
}

// TestMigrations_Applies proves [tasks.Migrations] returns the durable
// TaskStore's forward-only migration and that it applies cleanly through
// Store.Migrate and is idempotent on re-run.
func TestMigrations_Applies(t *testing.T) {
	t.Parallel()
	st := inmem.New()
	defer func() { _ = st.Close() }()
	if err := st.Migrate(context.Background(), tasks.Migrations()); err != nil {
		t.Fatalf("Migrate with tasks.Migrations(): %v", err)
	}
	// A re-run is idempotent — the forward-only migration runner skips it.
	if err := st.Migrate(context.Background(), tasks.Migrations()); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
}

// TestMigrations_FreshSetPerCall proves [tasks.Migrations] returns an
// independent set on every call — no shared mutable state — so concurrent
// fixtures never interfere (the S1 fix property).
func TestMigrations_FreshSetPerCall(t *testing.T) {
	t.Parallel()
	a := tasks.Migrations()
	b := tasks.Migrations()
	if a == b {
		t.Fatal("tasks.Migrations() returned the same set pointer twice — must be a fresh set per call")
	}
	if a.Len() != 1 || b.Len() != 1 {
		t.Fatalf("each Migrations() set must hold exactly 1 migration; got a=%d b=%d", a.Len(), b.Len())
	}
}
