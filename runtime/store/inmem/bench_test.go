package inmem_test

import (
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
	"github.com/hurtener/dockyard/runtime/store/inmem"
	"github.com/hurtener/dockyard/runtime/store/storetest"
)

// BenchmarkInmemStore runs the Phase 21.5 shared Store benchmark suite against
// the in-memory driver — the single-user stdio baseline. `make bench` runs it.
func BenchmarkInmemStore(b *testing.B) {
	storetest.RunBenchmarks(b, func() store.Store { return inmem.New() })
}
