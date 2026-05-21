// Package sqlitestore is the modernc.org/sqlite Store driver (RFC §13).
//
// It is the V1 durable persistence driver for HTTP and Portico-managed
// Dockyard apps. modernc.org/sqlite is a pure-Go, CGo-free port of SQLite3
// (brief 06 §2.8): the driver compiles and links with CGO_ENABLED=0 and
// cross-compiles cleanly to Dockyard's target triples (brief 06 §4 R6 —
// darwin/arm64, linux/amd64, linux/arm64, windows/amd64 are all supported; see
// decision D-026). Like every Store driver it passes the shared conformance
// suite in runtime/store/storetest.
//
// The driver registers itself under the name "sqlite" via its init block; a
// blank import wires it up:
//
//	import _ "github.com/hurtener/dockyard/runtime/store/sqlitestore"
//
// The data-source name passed to store.Open is a filesystem path. The special
// value ":memory:" opens a private in-memory SQLite database, useful for
// tests; an empty dsn is treated as ":memory:".
package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/hurtener/dockyard/runtime/store"
	_ "modernc.org/sqlite" // pure-Go, CGo-free SQLite database/sql driver
)

// DriverName is the name this driver registers under.
const DriverName = "sqlite"

// sqlDriverName is the database/sql driver name modernc.org/sqlite registers.
const sqlDriverName = "sqlite"

func init() {
	store.Register(DriverName, func(ctx context.Context, dsn string) (store.Store, error) {
		return Open(ctx, dsn)
	})
}

// sqliteStore is the modernc.org/sqlite-backed Store. The keyspace is a single
// table:
//
//	CREATE TABLE kv (ns TEXT, key TEXT, value BLOB, PRIMARY KEY (ns, key));
//
// A future sub-store (TaskStore, ObsStore) layers typed structure over this
// table through its own migrations; the driver itself is schema-agnostic.
type sqliteStore struct {
	db *sql.DB

	mu     sync.Mutex
	closed bool
}

// Open opens (creating if absent) a SQLite database at dsn and returns a
// Store. An empty dsn or ":memory:" opens a private in-memory database.
func Open(ctx context.Context, dsn string) (store.Store, error) {
	if dsn == "" {
		dsn = ":memory:"
	}
	// _pragma options enable WAL for durability+concurrency on file-backed
	// databases and a busy timeout so brief lock contention waits rather than
	// erroring. They are no-ops for :memory:.
	connDSN := dsn + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open(sqlDriverName, connDSN)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: open %q: %w", dsn, err)
	}
	// An in-memory SQLite database is per-connection; pinning the pool to a
	// single connection keeps every transaction on the same database. For
	// file-backed databases this also bounds writer contention, which SQLite
	// serializes anyway.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlitestore: ping %q: %w", dsn, err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS kv (
			ns    TEXT NOT NULL,
			key   TEXT NOT NULL,
			value BLOB NOT NULL,
			PRIMARY KEY (ns, key)
		) WITHOUT ROWID`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlitestore: create kv table: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *sqliteStore) Migrate(ctx context.Context, set *store.MigrationSet) error {
	return store.RunMigrations(ctx, s, set)
}

func (s *sqliteStore) View(ctx context.Context, fn func(store.Tx) error) error {
	return s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, fn)
}

func (s *sqliteStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return s.inTx(ctx, &sql.TxOptions{ReadOnly: false}, fn)
}

func (s *sqliteStore) inTx(ctx context.Context, opts *sql.TxOptions, fn func(store.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.isClosed() {
		return store.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("sqlitestore: begin tx: %w", err)
	}
	stx := &sqliteTx{ctx: ctx, tx: tx, writable: !opts.ReadOnly}
	if err := fn(stx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("sqlitestore: rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitestore: commit: %w", err)
	}
	return nil
}

func (s *sqliteStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.isClosed() {
		return store.ErrClosed
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlitestore: ping: %w", err)
	}
	return nil
}

func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("sqlitestore: close: %w", err)
	}
	return nil
}

// sqliteTx is a transaction over the kv table. It is not safe for concurrent
// use and is valid only within the View/Update callback.
type sqliteTx struct {
	ctx      context.Context
	tx       *sql.Tx
	writable bool
}

func (t *sqliteTx) Get(ns, key string) ([]byte, error) {
	var value []byte
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT value FROM kv WHERE ns = ? AND key = ?`, ns, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: get %q/%q: %w", ns, key, err)
	}
	return value, nil
}

func (t *sqliteTx) Put(ns, key string, value []byte) error {
	if !t.writable {
		return store.ErrReadOnly
	}
	// Store a non-nil blob so an empty value round-trips distinctly from a
	// missing key (the column is NOT NULL).
	v := value
	if v == nil {
		v = []byte{}
	}
	if _, err := t.tx.ExecContext(t.ctx,
		`INSERT INTO kv (ns, key, value) VALUES (?, ?, ?)
		 ON CONFLICT (ns, key) DO UPDATE SET value = excluded.value`,
		ns, key, v); err != nil {
		return fmt.Errorf("sqlitestore: put %q/%q: %w", ns, key, err)
	}
	return nil
}

func (t *sqliteTx) Delete(ns, key string) error {
	if !t.writable {
		return store.ErrReadOnly
	}
	if _, err := t.tx.ExecContext(t.ctx,
		`DELETE FROM kv WHERE ns = ? AND key = ?`, ns, key); err != nil {
		return fmt.Errorf("sqlitestore: delete %q/%q: %w", ns, key, err)
	}
	return nil
}

func (t *sqliteTx) Scan(ns, prefix string) ([]store.KeyValue, error) {
	// LIKE with an escaped prefix matches keys starting with prefix; ESCAPE
	// neutralises % and _ in the prefix itself.
	pattern := escapeLike(prefix) + "%"
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT key, value FROM kv
		 WHERE ns = ? AND key LIKE ? ESCAPE '\'
		 ORDER BY key ASC`, ns, pattern)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: scan %q/%q: %w", ns, prefix, err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.KeyValue
	for rows.Next() {
		var kv store.KeyValue
		if err := rows.Scan(&kv.Key, &kv.Value); err != nil {
			return nil, fmt.Errorf("sqlitestore: scan row: %w", err)
		}
		out = append(out, kv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitestore: scan rows: %w", err)
	}
	return out, nil
}

// escapeLike escapes the LIKE wildcards (%, _) and the escape character itself
// so a prefix containing them is matched literally.
func escapeLike(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '%', '_', '\\':
			b = append(b, '\\', c)
		default:
			b = append(b, c)
		}
	}
	return string(b)
}
