package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/runtime/obs"
)

// This file holds the Phase 21.5 concurrent-stress proof for LogBridge. The
// audit flagged LogBridge as a reusable concurrent artifact (its godoc claims
// "Log is safe from many goroutines") that lacked an explicit concurrent test.
// CLAUDE.md §5 requires a reusable artifact to be proven safe under concurrent
// use, and §11 requires -race on every run. These tests close that gap.

// TestLogBridge_ConcurrentLogIsRaceFree fans many goroutines through one
// LogBridge concurrently, each emitting log records, and asserts every record
// lands as an obs/v1 log event with no race and no lost event. The shared
// artifacts under stress are the single LogBridge and the obs.Recorder /
// RingBuffer behind it.
func TestLogBridge_ConcurrentLogIsRaceFree(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 16
		perG       = 64
		total      = goroutines * perG
	)
	// Ring capacity >= total so no event is overwritten — the test can assert
	// an exact count, not just "no race".
	ring := obs.NewRingBuffer(total)
	s := newLogTestServer(t, ring)
	b := s.LogBridge()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				// Vary level + logger so different code paths (level
				// normalisation, the logger field) run concurrently.
				rec := LogRecord{
					Level:   []LogLevel{LogDebug, LogInfo, LogWarning, LogError}[i%4],
					Logger:  "tool.concurrent",
					Message: "concurrent log record",
				}
				if err := b.Log(context.Background(), rec); err != nil {
					t.Errorf("goroutine %d: Log: %v", g, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	events := ring.Recent(0)
	if len(events) != total {
		t.Fatalf("concurrent Log: got %d obs events, want %d (lost or dropped)", len(events), total)
	}
	if dropped := ring.Dropped(); dropped != 0 {
		t.Fatalf("ring reported %d dropped events — capacity was sized to avoid this", dropped)
	}
	for i, ev := range events {
		if ev.Kind != obs.KindLog {
			t.Fatalf("event %d: kind = %q, want %q", i, ev.Kind, obs.KindLog)
		}
		var p obs.LogPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("event %d: decode LogPayload: %v", i, err)
		}
		if p.Message != "concurrent log record" || p.Logger != "tool.concurrent" {
			t.Fatalf("event %d: corrupt payload %+v — a concurrent write interleaved", i, p)
		}
	}
}

// TestLogBridge_ConcurrentBridgeHandlesAreIndependent proves that obtaining a
// LogBridge concurrently (Server.LogBridge constructs a fresh handle each call)
// is itself race-free, and that handles obtained from many goroutines all share
// the one server obs.Recorder correctly — every emitted record is observed.
func TestLogBridge_ConcurrentBridgeHandlesAreIndependent(t *testing.T) {
	t.Parallel()

	const goroutines = 24
	ring := obs.NewRingBuffer(goroutines)
	s := newLogTestServer(t, ring)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			// Each goroutine obtains its own bridge handle, then logs once.
			bridge := s.LogBridge()
			if err := bridge.Log(context.Background(), LogRecord{
				Level:   LogInfo,
				Message: "per-handle record",
			}); err != nil {
				t.Errorf("Log: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := len(ring.Recent(0)); got != goroutines {
		t.Fatalf("got %d obs events, want %d — a handle dropped its record", got, goroutines)
	}
}
