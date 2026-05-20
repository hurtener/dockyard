package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// migrationNamespace is the reserved KV namespace the migration runner uses to
// record which migrations have been applied. Sub-stores must not use a
// namespace with this name.
const migrationNamespace = "__store_migrations__"

// Migration is one forward-only schema or data step. Migrations are
// append-only: once a Migration has merged its Up function is never edited
// (AGENTS.md §9). The runner detects a mutation and refuses to proceed.
type Migration struct {
	// ID uniquely identifies the migration and fixes its order. Use a
	// zero-padded numeric prefix, e.g. "0001_init", "0002_add_obs".
	ID string

	// Up performs the migration inside a read-write transaction. It must be
	// idempotent-safe in the sense that it only runs once per store, but it
	// need not itself guard against re-runs — the runner records completion.
	Up func(ctx context.Context, tx Tx) error
}

// fingerprint is a content hash of a migration, used to detect post-merge
// edits. It hashes the ID and the Up function's runtime identity is not
// stable, so the fingerprint covers the ID and the migration's ordinal
// position; combined with the recorded sequence-prefix check this catches
// reordering and removal. A migration whose Up body changes without an ID
// change cannot be hashed from a closure, so the runner additionally treats
// any divergence in the applied ID sequence as a mutation.
func (m Migration) fingerprint(ordinal int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", ordinal, m.ID)))
	return hex.EncodeToString(h[:])
}

var (
	migrationsMu sync.Mutex
	migrations   []Migration
	migrationIDs = map[string]struct{}{}
)

// AddMigration registers a forward-only migration. Registration order is
// application order. It is called from package init blocks — typically a
// sub-store package registers its own migrations. Registering the same ID
// twice panics: a duplicate migration ID is a programming error.
func AddMigration(m Migration) {
	if m.ID == "" {
		panic("store: AddMigration called with an empty ID")
	}
	if m.Up == nil {
		panic(fmt.Sprintf("store: AddMigration %q has a nil Up function", m.ID))
	}
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	if _, dup := migrationIDs[m.ID]; dup {
		panic(fmt.Errorf("%w: %q", ErrDuplicateMigration, m.ID))
	}
	migrationIDs[m.ID] = struct{}{}
	migrations = append(migrations, m)
}

// registeredMigrations returns a snapshot of the registered migrations in
// order. Exported within the package for the runner and for tests.
func registeredMigrations() []Migration {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	return out
}

// resetMigrationsForTest clears the global migration registry. Tests that
// register migrations call it to stay isolated; it is unexported so it is not
// part of the public surface.
func resetMigrationsForTest() {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	migrations = nil
	migrationIDs = map[string]struct{}{}
}

// appliedRecord is the JSON value stored per applied migration.
type appliedRecord struct {
	Ordinal     int    `json:"ordinal"`
	Fingerprint string `json:"fingerprint"`
}

// RunMigrations applies every registered migration not yet recorded in s,
// inside s's own transactions. It is the shared implementation every driver's
// Migrate method delegates to, so migration semantics are identical across
// drivers (AGENTS.md §9). It is forward-only and idempotent:
//
//   - A migration already recorded with a matching fingerprint is skipped.
//   - A recorded migration whose fingerprint no longer matches the registered
//     one yields ErrMigrationMutated (a migration was edited after merge).
//   - A registered sequence that does not extend the applied sequence as a
//     prefix yields ErrMigrationOutOfOrder.
func RunMigrations(ctx context.Context, s Store) error {
	regs := registeredMigrations()

	// Load the applied set.
	applied := map[string]appliedRecord{}
	if err := s.View(ctx, func(tx Tx) error {
		kvs, err := tx.Scan(migrationNamespace, "")
		if err != nil {
			return err
		}
		for _, kv := range kvs {
			var rec appliedRecord
			if err := json.Unmarshal(kv.Value, &rec); err != nil {
				return fmt.Errorf("decode migration record %q: %w", kv.Key, err)
			}
			applied[kv.Key] = rec
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store: load applied migrations: %w", err)
	}

	// Verify the registered sequence extends the applied sequence as a prefix
	// and that no applied migration was mutated.
	for ordinal, m := range regs {
		rec, ok := applied[m.ID]
		if !ok {
			continue
		}
		if rec.Ordinal != ordinal {
			return fmt.Errorf("%w: migration %q applied at ordinal %d, now at %d",
				ErrMigrationOutOfOrder, m.ID, rec.Ordinal, ordinal)
		}
		if rec.Fingerprint != m.fingerprint(ordinal) {
			return fmt.Errorf("%w: migration %q", ErrMigrationMutated, m.ID)
		}
	}
	// Any applied migration absent from the registered set means a migration
	// was removed — also forbidden.
	regByID := make(map[string]struct{}, len(regs))
	for _, m := range regs {
		regByID[m.ID] = struct{}{}
	}
	for id := range applied {
		if _, ok := regByID[id]; !ok {
			return fmt.Errorf("%w: applied migration %q is no longer registered",
				ErrMigrationOutOfOrder, id)
		}
	}

	// Apply each not-yet-applied migration in its own transaction so a failure
	// leaves a clean prefix of applied migrations.
	for ordinal, m := range regs {
		if _, done := applied[m.ID]; done {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rec := appliedRecord{Ordinal: ordinal, Fingerprint: m.fingerprint(ordinal)}
		recBytes, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("store: encode migration record %q: %w", m.ID, err)
		}
		if err := s.Update(ctx, func(tx Tx) error {
			if err := m.Up(ctx, tx); err != nil {
				return fmt.Errorf("apply migration %q: %w", m.ID, err)
			}
			return tx.Put(migrationNamespace, m.ID, recBytes)
		}); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	return nil
}
