package store

import "errors"

// Sentinel errors returned across the Store seam. Callers match with
// errors.Is; drivers wrap these with %w to add context.
var (
	// ErrNotFound is returned by Tx.Get when the (namespace, key) pair has no
	// value.
	ErrNotFound = errors.New("store: key not found")

	// ErrUnknownDriver is returned by Open when no driver is registered under
	// the requested name.
	ErrUnknownDriver = errors.New("store: unknown driver")

	// ErrClosed is returned by any operation on a Store after Close.
	ErrClosed = errors.New("store: store is closed")

	// ErrMigrationMutated is returned by Migrate when a previously-applied
	// migration's recorded identity no longer matches the registered one — a
	// migration was edited after it merged, which is forbidden (AGENTS.md §9).
	ErrMigrationMutated = errors.New("store: applied migration was mutated")

	// ErrMigrationOutOfOrder is returned by Migrate when the registered
	// migration sequence does not extend the already-applied sequence as a
	// prefix — migrations are append-only and forward-only.
	ErrMigrationOutOfOrder = errors.New("store: migration registered out of order")

	// ErrDuplicateDriver is returned by Register when a driver name is
	// registered twice.
	ErrDuplicateDriver = errors.New("store: driver registered twice")

	// ErrDuplicateMigration is returned by AddMigration when a migration ID is
	// registered twice.
	ErrDuplicateMigration = errors.New("store: migration registered twice")
)
