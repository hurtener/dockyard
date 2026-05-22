package obs

import (
	"context"
	"testing"
)

// This file holds the Phase 21.5 benchmarks for the obs hot paths — the
// reusable concurrent artifacts on the runtime's emit path: the RingBuffer
// (emit + drain) and the FanOut emitter. Benchmarks are a baseline and a
// regression-spotting tool, not a CI gate; `make bench` runs them on demand.
// They are deliberately -race-free (a benchmark needs real numbers).

// BenchmarkRingBufferEmit measures the cost of a single non-blocking Emit into
// a steady-state (full) ring buffer — the runtime's per-event hot path.
func BenchmarkRingBufferEmit(b *testing.B) {
	r := NewRingBuffer(DefaultRingCapacity)
	e := mkEvent(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Emit(ctx, e)
	}
}

// BenchmarkRingBufferEmitParallel measures Emit under contention — many
// goroutines emitting into one ring, the realistic multi-handler shape.
func BenchmarkRingBufferEmitParallel(b *testing.B) {
	r := NewRingBuffer(DefaultRingCapacity)
	e := mkEvent(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.Emit(ctx, e)
		}
	})
}

// BenchmarkRingBufferRecent measures a full drain — the inspector pulling the
// retained history out of a full ring.
func BenchmarkRingBufferRecent(b *testing.B) {
	r := NewRingBuffer(DefaultRingCapacity)
	ctx := context.Background()
	for i := 0; i < DefaultRingCapacity; i++ {
		r.Emit(ctx, mkEvent(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Recent(0)
	}
}

// BenchmarkFanOutEmit measures the fan-out emitter dispatching one event to a
// small set of downstream emitters — the obs/v1 multi-transport shape (ring +
// SSE, say). The downstreams here are NopEmitters so the benchmark isolates
// the fan-out dispatch cost itself.
func BenchmarkFanOutEmit(b *testing.B) {
	fo := NewFanOut(NopEmitter{}, NopEmitter{}, NopEmitter{})
	e := mkEvent(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fo.Emit(ctx, e)
	}
}

// BenchmarkRecorderToolCall measures the Recorder's tool-call span path —
// start + the end closure — driving a real RingBuffer. This is the per-tool-
// call observability overhead a Dockyard server pays.
func BenchmarkRecorderToolCall(b *testing.B) {
	rec := NewRecorder(NewRingBuffer(DefaultRingCapacity), "bench-server")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc := NewTrace()
		end := rec.ToolCall(ctx, sc, "bench_tool", "stdio")
		end(nil, nil, nil)
	}
}
