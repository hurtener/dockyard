package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
)

// openStore opens an in-memory store and returns the concrete *sqliteStore so
// the DB-error branches can be exercised directly.
func openStore(t *testing.T) *sqliteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	ss, ok := s.(*sqliteStore)
	if !ok {
		t.Fatalf("Open returned %T, want *sqliteStore", s)
	}
	return ss
}

// breakDB closes the underlying *sql.DB while leaving the store's `closed`
// flag false, so subsequent operations slip past the isClosed() guard and hit
// the database/sql error branches (BeginTx, PingContext).
func breakDB(t *testing.T, s *sqliteStore) {
	t.Helper()
	if err := s.db.Close(); err != nil {
		t.Fatalf("closing underlying db: %v", err)
	}
}

func TestOpenEmptyDSNIsInMemory(t *testing.T) {
	// An empty dsn must be treated as ":memory:" — covers the dsn=="" branch.
	s, err := Open(context.Background(), "")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestInTxBeginError(t *testing.T) {
	s := openStore(t)
	breakDB(t, s)
	// inTx must surface the BeginTx failure (the db is closed underneath).
	err := s.Update(context.Background(), func(store.Tx) error { return nil })
	if err == nil {
		t.Fatal("Update on a broken db should fail at BeginTx")
	}
	if errors.Is(err, store.ErrClosed) {
		t.Fatalf("got ErrClosed, want the begin-tx error: %v", err)
	}
}

func TestInTxContextCancelled(t *testing.T) {
	s := openStore(t)
	defer func() { _ = s.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Update(ctx, func(store.Tx) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Update with cancelled ctx: got %v want context.Canceled", err)
	}
	if err := s.View(ctx, func(store.Tx) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("View with cancelled ctx: got %v want context.Canceled", err)
	}
}

func TestInTxClosedStore(t *testing.T) {
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Update(context.Background(), func(store.Tx) error { return nil }); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("Update after Close: got %v want ErrClosed", err)
	}
}

// TestInTxRollbackError covers the rollback-failure join branch: the callback
// fails (so inTx rolls back) while the transaction's context is already
// cancelled, which makes Rollback itself fail.
func TestInTxRollbackError(t *testing.T) {
	s := openStore(t)
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	sentinel := errors.New("callback failure")
	err := s.Update(ctx, func(store.Tx) error {
		cancel() // poison the tx context so the deferred Rollback fails
		return sentinel
	})
	// The callback error must still be reported (joined with the rollback
	// error when the rollback fails).
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update: got %v, want it to wrap the callback sentinel", err)
	}
}

// TestInTxCommitError covers the commit-failure branch: the callback succeeds
// but the transaction's context is cancelled, so Commit fails.
func TestInTxCommitError(t *testing.T) {
	s := openStore(t)
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	err := s.Update(ctx, func(tx store.Tx) error {
		if err := tx.Put("ns", "k", []byte("v")); err != nil {
			return err
		}
		cancel() // poison the tx context so the trailing Commit fails
		return nil
	})
	if err == nil {
		t.Fatal("Commit on a cancelled-context tx should fail")
	}
}

func TestPingContextCancelled(t *testing.T) {
	s := openStore(t)
	defer func() { _ = s.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ping with cancelled ctx: got %v want context.Canceled", err)
	}
}

func TestPingDBError(t *testing.T) {
	s := openStore(t)
	breakDB(t, s)
	// The closed flag is still false, so Ping reaches PingContext, which fails.
	err := s.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping on a broken db should fail")
	}
	if errors.Is(err, store.ErrClosed) {
		t.Fatalf("got ErrClosed, want the ping error: %v", err)
	}
}

func TestPingClosedStore(t *testing.T) {
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Ping(context.Background()); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("Ping after Close: got %v want ErrClosed", err)
	}
}

func TestTxPutReadOnly(t *testing.T) {
	tx := &sqliteTx{ctx: context.Background(), writable: false}
	if err := tx.Put("ns", "k", []byte("v")); !errors.Is(err, store.ErrReadOnly) {
		t.Fatalf("Put on a read-only tx: got %v want ErrReadOnly", err)
	}
}

func TestTxDeleteReadOnly(t *testing.T) {
	tx := &sqliteTx{ctx: context.Background(), writable: false}
	if err := tx.Delete("ns", "k"); !errors.Is(err, store.ErrReadOnly) {
		t.Fatalf("Delete on a read-only tx: got %v want ErrReadOnly", err)
	}
}

func TestTxPutNilValueRoundTrips(t *testing.T) {
	s := openStore(t)
	defer func() { _ = s.Close() }()
	// A nil value must be stored as an empty (non-nil) blob — covers the
	// v == nil branch in Put.
	if err := s.Update(context.Background(), func(tx store.Tx) error {
		return tx.Put("ns", "nilval", nil)
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.View(context.Background(), func(tx store.Tx) error {
		got, err := tx.Get("ns", "nilval")
		if err != nil {
			return err
		}
		if len(got) != 0 {
			t.Fatalf("nil value round-tripped as %q, want empty", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

// TestTxExecAndQueryErrors exercises the Exec/Query failure branches in Get,
// Put, Delete and Scan by cancelling the transaction's context mid-callback so
// the next database/sql call fails.
func TestTxExecAndQueryErrors(t *testing.T) {
	cases := []struct {
		name string
		op   func(store.Tx) error
		view bool
	}{
		{"Get", func(tx store.Tx) error { _, err := tx.Get("ns", "k"); return err }, true},
		{"Put", func(tx store.Tx) error { return tx.Put("ns", "k", []byte("v")) }, false},
		{"Delete", func(tx store.Tx) error { return tx.Delete("ns", "k") }, false},
		{"Scan", func(tx store.Tx) error { _, err := tx.Scan("ns", ""); return err }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t)
			defer func() { _ = s.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			run := s.Update
			if tc.view {
				run = s.View
			}
			err := run(ctx, func(tx store.Tx) error {
				cancel() // poison the tx context
				return tc.op(tx)
			})
			if err == nil {
				t.Fatalf("%s against a cancelled-context tx should fail", tc.name)
			}
		})
	}
}
