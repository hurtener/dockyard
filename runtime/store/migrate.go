package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// migrationNamespace is the reserved KV namespace the migration runner uses to
// record which migrations have been applied. Sub-stores must not use a
// namespace with this name.
const migrationNamespace = "__store_migrations__"

// Migration is one forward-only schema or data step. Migrations are
// append-only and forward-only: the runner detects a reorder or removal of an
// already-applied migration and refuses to proceed (ErrMigrationOutOfOrder).
//
// "Never edit a migration's Up body after it merges" (CLAUDE.md §9) is a
// REVIEW-ENFORCED rule, NOT a runtime-enforced one — and deliberately so. A
// Migration's effect is a Go func (Up), and a func value cannot be reliably
// content-hashed: Go offers no stable hash of a closure's body, and two
// closures over different source share an entry point. The runner therefore
// cannot detect an edit to an Up body that keeps the same ID and ordinal. It
// does not pretend to: it enforces ordering and non-removal at runtime, and the
// no-post-merge-edit rule is enforced in code review and CI diff (D-111, which
// supersedes D-027's runtime-enforcement claim).
type Migration struct {
	// ID uniquely identifies the migration and fixes its order. Use a
	// zero-padded numeric prefix, e.g. "0001_init", "0002_add_obs".
	ID string

	// Up performs the migration inside a read-write transaction. It must be
	// idempotent-safe in the sense that it only runs once per store, but it
	// need not itself guard against re-runs — the runner records completion.
	Up func(ctx context.Context, tx Tx) error
}

// MigrationSet is an explicit, caller-owned, ordered collection of migrations
// (D-073). It REPLACES the former process-global migration registry: a
// MigrationSet is a plain value a caller constructs, populates, and passes to
// [Store.Migrate], so there is no mutable shared state and no panic-on-duplicate
// global. Two goroutines — or two t.Parallel() test fixtures — each build their
// own MigrationSet and migrate independent stores with no cross-talk and no
// external locking.
//
// Registration order is application order. A MigrationSet is NOT safe for
// concurrent mutation (it is built once, by its owner, before use); it is safe
// to pass a fully-built set to many concurrent [Store.Migrate] calls because
// Migrate only reads it.
type MigrationSet struct {
	migrations []Migration
	ids        map[string]struct{}
}

// NewMigrationSet returns an empty [MigrationSet] ready for [MigrationSet.Add].
func NewMigrationSet() *MigrationSet {
	return &MigrationSet{ids: map[string]struct{}{}}
}

// Add appends a migration. It returns [ErrDuplicateMigration] for a repeated
// ID, and a descriptive error for an empty ID or a nil Up — a duplicate or
// malformed migration is a programming error, but Add returns it rather than
// panicking so a caller (a sub-store assembling its set, a test) handles it
// cleanly (CLAUDE.md §5: never panic for control flow). Add returns the
// receiver so calls chain.
func (s *MigrationSet) Add(m Migration) (*MigrationSet, error) {
	if m.ID == "" {
		return s, fmt.Errorf("store: MigrationSet.Add with an empty ID")
	}
	if m.Up == nil {
		return s, fmt.Errorf("store: MigrationSet.Add %q has a nil Up function", m.ID)
	}
	if _, dup := s.ids[m.ID]; dup {
		return s, fmt.Errorf("%w: %q", ErrDuplicateMigration, m.ID)
	}
	s.ids[m.ID] = struct{}{}
	s.migrations = append(s.migrations, m)
	return s, nil
}

// MustAdd is [MigrationSet.Add] for a caller that treats a duplicate/malformed
// migration as a build-time invariant — typically a sub-store assembling its
// own fixed set in a constructor. It panics on error. It is not used on any
// request path, so a panic here never crosses the MCP boundary (CLAUDE.md §13).
func (s *MigrationSet) MustAdd(m Migration) *MigrationSet {
	if _, err := s.Add(m); err != nil {
		panic(err)
	}
	return s
}

// Extend appends every migration of other into s, preserving order. It is how a
// caller composes the migration sets of several sub-stores (e.g. the TaskStore
// set plus a future ObsStore set) into the one set it hands [Store.Migrate]. A
// duplicate ID across the sets yields [ErrDuplicateMigration].
func (s *MigrationSet) Extend(other *MigrationSet) (*MigrationSet, error) {
	if other == nil {
		return s, nil
	}
	for _, m := range other.migrations {
		if _, err := s.Add(m); err != nil {
			return s, err
		}
	}
	return s, nil
}

// Len reports the number of migrations in the set.
func (s *MigrationSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.migrations)
}

// list returns the migrations in application order. The caller must not mutate
// the returned slice.
func (s *MigrationSet) list() []Migration {
	if s == nil {
		return nil
	}
	return s.migrations
}

// appliedRecord is the JSON value stored per applied migration. It records the
// ordinal position the migration was applied at, so the runner can detect a
// post-merge reorder. It deliberately stores no content hash of the migration's
// Up function: a Go func cannot be reliably content-hashed (see [Migration]),
// so a per-migration fingerprint could never detect an edited Up body and would
// be a false guarantee. Detecting an edited Up body is a review-enforced rule
// (D-111).
type appliedRecord struct {
	Ordinal int `json:"ordinal"`
}

// RunMigrations applies every migration of set not yet recorded in s, inside
// s's own transactions. It is the shared implementation every driver's Migrate
// method delegates to, so migration semantics are identical across drivers
// (CLAUDE.md §9). A nil set is a valid no-op. It is forward-only and idempotent:
//
//   - A migration already recorded at its current ordinal is skipped.
//   - A recorded migration now registered at a different ordinal, or an applied
//     migration no longer registered at all, yields ErrMigrationOutOfOrder — a
//     migration was reordered or removed after merge.
//
// It does NOT detect an edit to a migration's Up body that keeps the same ID
// and ordinal: a Go func cannot be content-hashed (see [Migration]), so that
// rule is review-enforced, not runtime-enforced (D-111).
func RunMigrations(ctx context.Context, s Store, set *MigrationSet) error {
	regs := set.list()

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
	// and that no applied migration was reordered.
	for ordinal, m := range regs {
		rec, ok := applied[m.ID]
		if !ok {
			continue
		}
		if rec.Ordinal != ordinal {
			return fmt.Errorf("%w: migration %q applied at ordinal %d, now at %d",
				ErrMigrationOutOfOrder, m.ID, rec.Ordinal, ordinal)
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
		rec := appliedRecord{Ordinal: ordinal}
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
