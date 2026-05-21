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

// openDurable builds a fresh durable TaskStore over a fresh backing Store. It
// isolates the global migration registry per call so the durable TaskStore's
// own forward-only migration applies cleanly against a clean store.
func openDurable(t *testing.T, mk func() store.Store) taskstoretest.OpenFunc {
	t.Helper()
	return func() tasks.TaskStore {
		store.ResetMigrationsForTest()
		tasks.RegisterMigrations()
		st := mk()
		if err := st.Migrate(context.Background()); err != nil {
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
	store.ResetMigrationsForTest()
	t.Cleanup(store.ResetMigrationsForTest)
	taskstoretest.RunConformance(t, openDurable(t, func() store.Store { return inmem.New() }))
}

// TestDurableTaskStore_OverSQLiteStore runs the shared TaskStore conformance
// suite against the durable facade layered over the modernc.org/sqlite Store
// driver — the V1 durable backing. modernc.org/sqlite is pure-Go; no CGo
// dependency is introduced (brief 06 §2.8, D-026).
func TestDurableTaskStore_OverSQLiteStore(t *testing.T) {
	store.ResetMigrationsForTest()
	t.Cleanup(store.ResetMigrationsForTest)
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
	if _, err := tasks.NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) must return an error")
	}
}

// TestRegisterMigrations_Applies proves RegisterMigrations registers the
// durable TaskStore's forward-only migration and that it applies cleanly
// through Store.Migrate.
func TestRegisterMigrations_Applies(t *testing.T) {
	store.ResetMigrationsForTest()
	t.Cleanup(store.ResetMigrationsForTest)
	tasks.RegisterMigrations()
	st := inmem.New()
	defer func() { _ = st.Close() }()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate after RegisterMigrations: %v", err)
	}
	// A re-run is idempotent — the forward-only migration runner skips it.
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("re-run Migrate: %v", err)
	}
}
