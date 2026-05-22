package storetest

import (
	"fmt"
	"testing"

	"github.com/hurtener/dockyard/runtime/store"
)

// This file holds the Phase 21.5 shared Store benchmark suite. Like
// RunConformance, it is defined once and every driver runs it — a Store driver
// is a hot reusable artifact (every durable read and write crosses it), so a
// per-driver baseline is worth keeping. Benchmarks are run on demand
// (`make bench`), never a CI gate.

// RunBenchmarks exercises the Store's common operations under benchmark.
// open must return a freshly-constructed, empty Store on each call; the suite
// closes each Store it opens. A driver's *_test.go calls this from a
// BenchmarkXxx function.
func RunBenchmarks(b *testing.B, open func() store.Store) {
	b.Helper()
	b.Run("Put", func(b *testing.B) { benchPut(b, open) })
	b.Run("Get", func(b *testing.B) { benchGet(b, open) })
	b.Run("Update", func(b *testing.B) { benchUpdate(b, open) })
	b.Run("View", func(b *testing.B) { benchView(b, open) })
	b.Run("Scan100", func(b *testing.B) { benchScan(b, open, 100) })
}

const benchNS = "bench"

// benchPut measures a single keyed write inside its own Update transaction —
// the per-write cost a caller pays.
func benchPut(b *testing.B, open func() store.Store) {
	s := open()
	defer func() { _ = s.Close() }()
	val := []byte("benchmark-value-payload")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("k-%d", i)
		if err := s.Update(ctx(), func(tx store.Tx) error {
			return tx.Put(benchNS, key, val)
		}); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
}

// benchGet measures a single keyed read inside a View transaction.
func benchGet(b *testing.B, open func() store.Store) {
	s := open()
	defer func() { _ = s.Close() }()
	if err := s.Update(ctx(), func(tx store.Tx) error {
		return tx.Put(benchNS, "hot", []byte("benchmark-value-payload"))
	}); err != nil {
		b.Fatalf("seed Put: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.View(ctx(), func(tx store.Tx) error {
			_, err := tx.Get(benchNS, "hot")
			return err
		}); err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// benchUpdate measures an empty read-write transaction — the transaction
// machinery cost alone, isolated from any key operation.
func benchUpdate(b *testing.B, open func() store.Store) {
	s := open()
	defer func() { _ = s.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Update(ctx(), func(store.Tx) error { return nil }); err != nil {
			b.Fatalf("Update: %v", err)
		}
	}
}

// benchView measures an empty read-only transaction.
func benchView(b *testing.B, open func() store.Store) {
	s := open()
	defer func() { _ = s.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.View(ctx(), func(store.Tx) error { return nil }); err != nil {
			b.Fatalf("View: %v", err)
		}
	}
}

// benchScan measures a prefix Scan over a namespace pre-loaded with n entries —
// the cost a list-shaped read (tasks/list, recent obs history) pays.
func benchScan(b *testing.B, open func() store.Store, n int) {
	s := open()
	defer func() { _ = s.Close() }()
	if err := s.Update(ctx(), func(tx store.Tx) error {
		for i := 0; i < n; i++ {
			if err := tx.Put(benchNS, fmt.Sprintf("e-%05d", i), []byte("v")); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatalf("seed Scan: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.View(ctx(), func(tx store.Tx) error {
			_, err := tx.Scan(benchNS, "e-")
			return err
		}); err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}
