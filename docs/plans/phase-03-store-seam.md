# Phase 03 — Store seam + sqlite/inmem drivers + conformance

## Summary

Establishes `runtime/store` — the `Store` interface (interface + factory + driver
pattern, `AGENTS.md` §4.4/§9), an in-memory driver, a `modernc.org/sqlite` driver
(pure-Go, CGo-free), a forward-only migration runner, and a shared conformance suite
every driver must pass. V1 delivers the seam, the two drivers, and the conformance
suite only — not the `TaskStore` or `ObsStore` sub-stores, which later phases build
on top of this seam.

## RFC anchor

- RFC §13 — Persistence & the storage seam.
- RFC §8.5 — the TaskStore (future consumer of this seam; the seam is shaped to
  host it but Phase 03 does not implement it).

## Briefs informing this phase

- brief 06 — Go-2026 no-CGo stack & toolchain.
- brief 02 — MCP Tasks extension (TaskStore is a future consumer of this seam).

## Brief findings incorporated

- brief 06 §2.8 / §3 toolchain table: "The one place CGo classically sneaks in is
  SQLite. … the answer is `modernc.org/sqlite` — a complete CGo-free pure-Go port of
  SQLite3 … a standard `database/sql` driver, cross-compiles cleanly." The sqlite
  driver uses `modernc.org/sqlite` exclusively and the build is verified under
  `CGO_ENABLED=0`.
- brief 06 §4 R6: "`modernc.org/sqlite` supports a fixed OS/arch set; verify it
  covers Dockyard's target triples." The phase plan and decisions log record the
  supported-triple verification as a constraint; the smoke script asserts the
  CGo-free build.
- brief 06 §4 R7: "Any future dependency may transitively pull CGo. CI must
  hard-fail builds unless `CGO_ENABLED=0`." The validation gate and smoke script
  build with `CGO_ENABLED=0`.
- brief 02 §3 (the `TaskStore` interface sketch) and §4.6: durable task state is
  pluggable persistence — "in-memory for stdio single-user apps; durable for
  HTTP/Portico-managed apps." The `Store` seam is designed so a future `TaskStore`
  (RFC §8.5) and `ObsStore` (RFC §11) attach as sub-stores without changing the
  driver contract: both drivers expose the same namespaced, transactional
  key-value primitive the sub-stores will build on.
- brief 02 §4.6: persistence is a leak risk — sub-stores need purge sweeps. The
  seam's `KV` primitive supports range scans and bulk delete so a future TTL
  sweeper is implementable on either driver without a new driver method.

## Findings I'm departing from (if any)

- RFC §13's illustrative interface shows `Store` exposing `Tasks() TaskStore` and
  `Obs() ObsStore` directly. Phase 03's scope (per the master-plan stub and the
  phase brief) is **the seam and drivers only — not Tasks or Obs content**. Shipping
  `Tasks()`/`Obs()` accessors now would force Phase 03 to define `TaskStore` /
  `ObsStore`, which belong to Phases 14 and 15. Instead the V1 `Store` interface
  exposes a generic, namespaced, transactional `KV` primitive plus migration and
  lifecycle methods; the future `TaskStore` and `ObsStore` are thin typed facades
  constructed over a `Store` (e.g. `tasks.NewStore(store)`), each owning its own
  forward-only migrations registered through the seam's migration registry. This is
  an interface-shape decision, not a design change — RFC §13's intent ("a future
  driver implements the same interface; a new persistence concern is proven by the
  conformance suite") is preserved. Filed as **D-025**.

## Goals

- A `Store` interface in `runtime/store` following the interface + factory + driver
  pattern; drivers register via `init()` blank-import (`AGENTS.md` §4.4).
- An in-memory driver (`runtime/store/inmem`) for single-user stdio apps.
- A `modernc.org/sqlite` driver (`runtime/store/sqlitestore`), pure-Go, CGo-free.
- A forward-only migration runner: migrations are append-only, ordered, idempotent
  on re-run, and never edited after merge.
- A shared conformance suite (`runtime/store/storetest`) every driver must pass,
  exercising CRUD, namespacing, transactions, range scans, migrations, and
  concurrency.
- The seam is broad enough that the future `TaskStore` (RFC §8.5) and `ObsStore`
  (RFC §11) attach as sub-stores without a driver-contract change.

## Non-goals

- The `TaskStore` and its TTL sweeper / concurrency caps — Phase 14 (RFC §8.5).
- The `ObsStore` and `obs/v1` history persistence — Phase 15 (RFC §11).
- A Postgres (or any non-sqlite, non-inmem) driver — future, post-V1 (RFC §13).
- Inspector state persistence — the relevant inspector phase.
- Connection pooling tuning beyond CGo-free correctness; encryption at rest.

## Acceptance criteria

- [ ] Both drivers (`inmem`, `sqlitestore`) pass the shared conformance suite.
- [ ] The build is CGo-free: `CGO_ENABLED=0 go build ./...` succeeds and the
      `modernc.org/sqlite` driver compiles under it.
- [ ] A concurrent-reuse test passes under `-race` — a single `Store` is safe under
      concurrent goroutine use for both drivers.
- [ ] Migration idempotency is verified: a clean `Migrate` then a re-run `Migrate`
      both succeed and leave identical schema state; an out-of-order or mutated
      migration is rejected.
- [ ] `runtime/store` and the two drivers meet the 85% coverage target.
- [ ] `scripts/smoke/phase-03.sh` reports `OK ≥ count(criteria)` and `FAIL = 0`.

## Files added or changed

```text
docs/plans/phase-03-store-seam.md          (new — this plan)
docs/decisions.md                          (D-025, D-026, D-027 appended)
docs/glossary.md                            (Store driver, conformance suite, KV namespace, forward-only migration)
scripts/smoke/phase-03.sh                   (new)
go.mod / go.sum                             (+ modernc.org/sqlite)
runtime/store/store.go                      (Store, KV, Tx interfaces; factory; driver registry)
runtime/store/migrate.go                    (forward-only migration runner + registry)
runtime/store/errors.go                     (sentinel errors)
runtime/store/store_test.go                 (factory/registry unit tests)
runtime/store/inmem/inmem.go                (in-memory driver)
runtime/store/inmem/inmem_test.go           (driver runs the conformance suite)
runtime/store/sqlitestore/sqlitestore.go    (modernc.org/sqlite driver)
runtime/store/sqlitestore/sqlitestore_test.go (driver runs the conformance suite)
runtime/store/storetest/conformance.go      (the shared conformance suite)
test/integration/store_seam_test.go         (both real drivers behind the seam)
```

`runtime/store` is a new package under the existing `runtime/` tree named in
`AGENTS.md` §3 — no new top-level directory, so `AGENTS.md` §3 is unchanged.

## Public API surface

```go
package store

// Store is the persistence seam. V1 drivers: inmem, sqlitestore.
type Store interface {
    // Migrate applies every not-yet-applied migration in registration order.
    // Forward-only and idempotent: a clean run and a re-run are both safe.
    Migrate(ctx context.Context) error
    // View runs fn in a read-only transaction.
    View(ctx context.Context, fn func(Tx) error) error
    // Update runs fn in a read-write transaction; fn's error rolls back.
    Update(ctx context.Context, fn func(Tx) error) error
    // Ping verifies the store is reachable.
    Ping(ctx context.Context) error
    // Close releases all resources.
    Close() error
}

// Tx is a namespaced key-value transaction handle. Future sub-stores
// (TaskStore §8.5, ObsStore §11) build on this primitive.
type Tx interface {
    Get(ns, key string) ([]byte, error)        // ErrNotFound if absent
    Put(ns, key string, value []byte) error
    Delete(ns, key string) error               // no-op if absent
    Scan(ns, prefix string) ([]KeyValue, error) // ordered by key
}

type KeyValue struct {
    Key   string
    Value []byte
}

// Open constructs a Store by driver name. Drivers register via init().
func Open(ctx context.Context, driver, dsn string) (Store, error)

// Register adds a driver factory. Called from a driver's init().
func Register(name string, factory func(ctx context.Context, dsn string) (Store, error))

// Migration is one forward-only schema/data step.
type Migration struct {
    ID  string // unique, ordered (e.g. "0001_init")
    Up  func(ctx context.Context, tx Tx) error
}

// AddMigration registers a migration. Order is registration order.
func AddMigration(m Migration)

// Sentinel errors: ErrNotFound, ErrUnknownDriver, ErrClosed,
// ErrMigrationMutated, ErrMigrationOutOfOrder.
```

```go
package storetest
// RunConformance exercises a freshly-opened Store against every seam
// guarantee. Each driver's test calls it.
func RunConformance(t *testing.T, open func() store.Store)
```

## Test plan

- **Unit:** `runtime/store` — factory/registry behaviour (`Open` unknown driver,
  duplicate `Register`), the migration runner (ordering, idempotency, mutation
  detection, out-of-order rejection), sentinel-error mapping.
- **Conformance:** `runtime/store/storetest.RunConformance` — CRUD round-trips,
  namespace isolation, `ErrNotFound`, transaction commit/rollback, ordered range
  scans, `Migrate` idempotency, post-`Close` behaviour. Both `inmem` and
  `sqlitestore` run it (`AGENTS.md` §9 — every driver proves the seam).
- **Integration:** `test/integration/store_seam_test.go` — opens both real drivers
  through `store.Open`, runs migrations, performs a cross-transaction read/write
  round-trip, and exercises one failure mode (a rolled-back `Update`). Real drivers
  on the seam, no mocks (`AGENTS.md` §17 — this phase opens a cross-subsystem seam
  later phases build on).
- **Concurrency:** a `-race` test driving concurrent `Update`/`View`/`Scan` on a
  single shared `Store` for each driver — proves the reusable-artifact guarantee
  (`AGENTS.md` §5, §14).
- **Golden:** N/A — no codegen output in this phase.

## Smoke script additions

`scripts/smoke/phase-03.sh` asserts:

- `runtime/store` package exists.
- Both drivers (`runtime/store/inmem`, `runtime/store/sqlitestore`) exist.
- The conformance suite (`runtime/store/storetest`) exists.
- `CGO_ENABLED=0 go build ./runtime/store/...` succeeds (CGo-free build).
- `CGO_ENABLED=0 go test ./runtime/store/...` passes (conformance + concurrency).
- `modernc.org/sqlite` is present in `go.mod`.

All checks `skip()` gracefully if the surface is absent (`common.sh` convention).

## Coverage target

- `runtime/store` — 85% (Store-seam package, `AGENTS.md` §11).
- `runtime/store/inmem` — 85%.
- `runtime/store/sqlitestore` — 85%.
- `runtime/store/storetest` — exercised by the driver tests; not a coverage target
  in itself (it is test scaffolding).

## Dependencies

- Phase 00 — repo skeleton & hygiene (shipped).

## Risks / open questions

- **`modernc.org/sqlite` cross-compile matrix** (brief 06 R6). The driver must
  cover darwin/arm64, linux/amd64, linux/arm64, windows/amd64. `modernc.org/sqlite`
  supports all four; recorded in D-026. Cross-compile verification is a later
  release-engineering phase concern; Phase 03 verifies the host build is CGo-free.
- **CGo creep** (brief 06 R7). A transitive dependency could pull CGo. Mitigation:
  the smoke script and validation gate build under `CGO_ENABLED=0`.
- **Seam shape vs. RFC §13's illustrative interface.** RFC §13 sketches
  `Tasks()`/`Obs()` accessors; Phase 03 ships a generic `KV` seam instead so it does
  not have to define sub-stores out of scope. Resolved and filed as D-025; the RFC
  intent is preserved.
- RFC §18 carries no open question specific to the `Store` seam (Q-5 — "does V1 need
  persistence" — is resolved by RFC §13 settling `modernc.org/sqlite`).

## Glossary additions

- **Store driver** — a concrete implementation of the `Store` seam registered via
  `init()` blank-import; V1 ships `inmem` and `sqlitestore`.
- **Conformance suite** — the shared `storetest` test battery every `Store` driver
  must pass, so a new driver is proven against the seam's guarantees.
- **KV namespace** — a logical partition of the `Store`'s key space; future
  sub-stores (`TaskStore`, `ObsStore`) each own one or more namespaces.
- **Forward-only migration** — an append-only, ordered, idempotent schema/data step;
  once merged a migration is never edited (`AGENTS.md` §9).

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (`AGENTS.md` §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`
