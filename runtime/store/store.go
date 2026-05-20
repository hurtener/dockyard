// Package store is the Dockyard persistence seam (RFC §13).
//
// All durable state — the future TaskStore (RFC §8.5), obs/v1 history
// (RFC §11), and inspector state — flows through the Store interface. The seam
// follows the interface + factory + driver pattern mandated by AGENTS.md §4.4:
// a driver registers a factory in its init block, and store.Open constructs a
// Store by driver name. V1 ships two drivers — the in-memory driver
// (runtime/store/inmem) for single-user stdio apps and the modernc.org/sqlite
// driver (runtime/store/sqlitestore), pure-Go and CGo-free.
//
// The seam is deliberately generic. Rather than expose Tasks() / Obs()
// accessors directly (which would force this package to define out-of-scope
// sub-store types), Store exposes a namespaced, transactional key-value
// primitive. Future sub-stores — the TaskStore and ObsStore — are thin typed
// facades constructed over a Store, each owning its own forward-only migrations
// registered through AddMigration. See decision D-025.
//
// Every driver must pass the shared conformance suite in
// runtime/store/storetest; a new persistence concern adds a migration and is
// proven by that suite, never bolted onto one driver (AGENTS.md §9).
package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Store is the Dockyard persistence seam. A Store is a reusable artifact: a
// single value must be safe for concurrent use by multiple goroutines
// (AGENTS.md §5).
type Store interface {
	// Migrate applies every registered migration that has not yet been
	// applied, in registration order. Migrations are forward-only and
	// idempotent: a clean run and any later re-run are both safe and leave
	// identical schema state. Migrate returns ErrMigrationMutated or
	// ErrMigrationOutOfOrder if the registered sequence diverges from what was
	// previously applied.
	Migrate(ctx context.Context) error

	// View runs fn inside a read-only transaction. A non-nil error from fn is
	// returned to the caller; no writes are possible.
	View(ctx context.Context, fn func(Tx) error) error

	// Update runs fn inside a read-write transaction. If fn returns a non-nil
	// error the transaction is rolled back and the error is returned;
	// otherwise it is committed.
	Update(ctx context.Context, fn func(Tx) error) error

	// Ping verifies the store is reachable and usable.
	Ping(ctx context.Context) error

	// Close releases all resources held by the Store. Operations after Close
	// return ErrClosed. Close is idempotent.
	Close() error
}

// Tx is a namespaced key-value transaction handle. It is the primitive every
// future sub-store (TaskStore §8.5, ObsStore §11) builds on. A Tx is not safe
// for concurrent use and must not be retained beyond the View/Update callback
// that produced it.
type Tx interface {
	// Get returns the value stored under (ns, key), or ErrNotFound if absent.
	// The returned slice is owned by the caller.
	Get(ns, key string) ([]byte, error)

	// Put stores value under (ns, key), overwriting any existing value. The
	// value slice is copied; the caller may reuse it after Put returns.
	Put(ns, key string, value []byte) error

	// Delete removes (ns, key). It is a no-op if the key is absent.
	Delete(ns, key string) error

	// Scan returns every key/value in ns whose key has the given prefix,
	// ordered lexicographically by key. An empty prefix scans the whole
	// namespace.
	Scan(ns, prefix string) ([]KeyValue, error)
}

// KeyValue is one entry returned by Tx.Scan.
type KeyValue struct {
	Key   string
	Value []byte
}

// Factory constructs a Store for a given data-source name. The dsn is
// driver-specific: a filesystem path for the sqlite driver, ignored by the
// in-memory driver.
type Factory func(ctx context.Context, dsn string) (Store, error)

var (
	driversMu sync.RWMutex
	drivers   = map[string]Factory{}
)

// Register adds a driver factory under name. It is called from a driver
// package's init block. Registering the same name twice panics — a duplicate
// registration is a programming error, caught at process start.
func Register(name string, factory Factory) {
	if factory == nil {
		panic("store: Register called with a nil factory")
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, dup := drivers[name]; dup {
		panic(fmt.Errorf("%w: %q", ErrDuplicateDriver, name))
	}
	drivers[name] = factory
}

// Drivers returns the names of all registered drivers, sorted.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Open constructs a Store using the named driver. The driver package must be
// imported (typically a blank import) so its init block has registered the
// factory. Open returns ErrUnknownDriver if no such driver is registered.
//
// Open does not run migrations; the caller invokes Store.Migrate explicitly so
// migration timing is under application control.
func Open(ctx context.Context, driver, dsn string) (Store, error) {
	driversMu.RLock()
	factory, ok := drivers[driver]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %v)", ErrUnknownDriver, driver, Drivers())
	}
	s, err := factory(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open driver %q: %w", driver, err)
	}
	return s, nil
}
