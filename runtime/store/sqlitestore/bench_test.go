package sqlitestore_test

import (
	"context"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
	"github.com/hurtener/dockyard/runtime/store/storetest"
)

// BenchmarkSqliteStore runs the Phase 21.5 shared Store benchmark suite against
// the SQLite driver — the durable V1 default — backed by an in-memory SQLite
// database so the benchmark measures the driver, not disk latency. `make bench`
// runs it.
func BenchmarkSqliteStore(b *testing.B) {
	storetest.RunBenchmarks(b, func() store.Store {
		s, err := sqlitestore.Open(context.Background(), ":memory:")
		if err != nil {
			b.Fatalf("Open(:memory:): %v", err)
		}
		return s
	})
}
