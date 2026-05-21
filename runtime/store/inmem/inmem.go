// Package inmem is the in-memory Store driver (RFC §13).
//
// It backs single-user stdio Dockyard apps, where durable persistence is
// unnecessary, and is the fast path for tests. All state lives in process
// memory and is lost on Close. Like every Store driver it passes the shared
// conformance suite in runtime/store/storetest.
//
// The driver registers itself under the name "inmem" via its init block; a
// blank import is enough to make store.Open("inmem", …) work:
//
//	import _ "github.com/hurtener/dockyard/runtime/store/inmem"
package inmem

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/hurtener/dockyard/runtime/store"
)

// DriverName is the name this driver registers under.
const DriverName = "inmem"

func init() {
	store.Register(DriverName, func(_ context.Context, _ string) (store.Store, error) {
		return New(), nil
	})
}

// memStore is the in-memory Store. A single RWMutex guards the whole keyspace;
// View takes a read lock and Update a write lock, which gives transactions
// full serializable isolation — adequate for the in-memory driver's role.
type memStore struct {
	mu     sync.RWMutex
	closed bool
	// data maps namespace -> key -> value. Values are stored as copies.
	data map[string]map[string][]byte
}

// New constructs an empty in-memory Store.
func New() store.Store {
	return &memStore{data: map[string]map[string][]byte{}}
}

func (s *memStore) Migrate(ctx context.Context, set *store.MigrationSet) error {
	return store.RunMigrations(ctx, s, set)
}

func (s *memStore) View(ctx context.Context, fn func(store.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return store.ErrClosed
	}
	return fn(&memTx{store: s, writable: false, staged: nil})
}

func (s *memStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return store.ErrClosed
	}
	// Stage writes so a failing fn rolls back cleanly.
	tx := &memTx{store: s, writable: true, staged: map[nsKey]*[]byte{}}
	if err := fn(tx); err != nil {
		return err
	}
	for nk, val := range tx.staged {
		if val == nil {
			if ns := s.data[nk.ns]; ns != nil {
				delete(ns, nk.key)
			}
			continue
		}
		ns := s.data[nk.ns]
		if ns == nil {
			ns = map[string][]byte{}
			s.data[nk.ns] = ns
		}
		ns[nk.key] = *val
	}
	return nil
}

func (s *memStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return store.ErrClosed
	}
	return nil
}

func (s *memStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.data = nil
	return nil
}

type nsKey struct {
	ns  string
	key string
}

// memTx is a transaction over memStore. For an Update transaction writes are
// staged in `staged` (a nil pointer marks a deletion) and committed by the
// enclosing Update only if fn succeeds. Reads consult the staged set first so
// a transaction sees its own writes.
type memTx struct {
	store    *memStore
	writable bool
	staged   map[nsKey]*[]byte
}

func (t *memTx) Get(ns, key string) ([]byte, error) {
	if t.staged != nil {
		if v, ok := t.staged[nsKey{ns, key}]; ok {
			if v == nil {
				return nil, store.ErrNotFound
			}
			return cloneBytes(*v), nil
		}
	}
	if m := t.store.data[ns]; m != nil {
		if v, ok := m[key]; ok {
			return cloneBytes(v), nil
		}
	}
	return nil, store.ErrNotFound
}

func (t *memTx) Put(ns, key string, value []byte) error {
	if !t.writable {
		return store.ErrReadOnly
	}
	v := cloneBytes(value)
	t.staged[nsKey{ns, key}] = &v
	return nil
}

func (t *memTx) Delete(ns, key string) error {
	if !t.writable {
		return store.ErrReadOnly
	}
	t.staged[nsKey{ns, key}] = nil
	return nil
}

func (t *memTx) Scan(ns, prefix string) ([]store.KeyValue, error) {
	// Merge the committed namespace with the transaction's staged writes.
	merged := map[string][]byte{}
	for k, v := range t.store.data[ns] {
		merged[k] = v
	}
	if t.staged != nil {
		for nk, v := range t.staged {
			if nk.ns != ns {
				continue
			}
			if v == nil {
				delete(merged, nk.key)
			} else {
				merged[nk.key] = *v
			}
		}
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]store.KeyValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, store.KeyValue{Key: k, Value: cloneBytes(merged[k])})
	}
	return out, nil
}

// cloneBytes returns an independent copy so callers cannot mutate stored data
// and stored data cannot be mutated by callers. A nil slice clones to a
// non-nil empty slice so a stored empty value round-trips distinctly from a
// missing key.
func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
