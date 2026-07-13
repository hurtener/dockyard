package tasks

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// This file holds the manifest-tunable task lifecycle controls (RFC §8.5): an
// enforced max TTL, a per-requestor concurrent-task cap, and the background TTL
// purge sweep. The values originate in the dockyard.app.yaml `tasks` block
// (internal/manifest); the Engine takes them as a Lifecycle option.

// Lifecycle is the set of manifest-tunable task-lifecycle limits (RFC §8.5).
// The zero value disables every limit — unlimited TTL, no concurrency cap, no
// purge sweep — which is the correct default for an ephemeral in-memory
// single-user stdio app. A durable HTTP/Portico app sets explicit limits from
// its manifest `tasks` block.
type Lifecycle struct {
	// MaxTTL is the largest retention duration the runtime honours. A requestor
	// asking for more is clamped down to MaxTTL; zero means unlimited (no
	// clamp). A task's enforced TTL is recorded on TaskRecord.TTL.
	MaxTTL time.Duration

	// DefaultTTL is the retention applied to a task whose requestor expressed no
	// TTL preference. Zero means unlimited retention by default.
	DefaultTTL time.Duration

	// PurgeInterval is how often the background TTL purge sweep runs. Zero
	// disables the sweep entirely — appropriate when MaxTTL is also zero.
	PurgeInterval time.Duration

	// MaxConcurrentPerRequestor caps the number of non-terminal tasks one
	// authorization context may hold at once — the brief 02 §4.6 resource-
	// exhaustion guard. Zero means no cap.
	MaxConcurrentPerRequestor int
}

// LifecycleFromMillis builds a Lifecycle from the millisecond-denominated
// values the dockyard.app.yaml `tasks` block carries (internal/manifest.Tasks).
// It is the manifest→runtime mapping: the manifest package deliberately does
// not depend on the runtime (one-way dependency), so an app's wiring code calls
// this to translate the loaded manifest block into the engine's Lifecycle. A
// zero millisecond value maps to a zero Duration — "no limit".
func LifecycleFromMillis(maxTTLMillis, defaultTTLMillis, purgeIntervalMillis int64, maxConcurrentPerRequestor int) Lifecycle {
	ms := func(v int64) time.Duration {
		if v <= 0 {
			return 0
		}
		return time.Duration(v) * time.Millisecond
	}
	return Lifecycle{
		MaxTTL:                    ms(maxTTLMillis),
		DefaultTTL:                ms(defaultTTLMillis),
		PurgeInterval:             ms(purgeIntervalMillis),
		MaxConcurrentPerRequestor: maxConcurrentPerRequestor,
	}
}

// enforcedTTL returns the TTL (in milliseconds, as the wire and TaskRecord
// represent it) the runtime will honour for a task whose requestor asked for
// requested, applying DefaultTTL and clamping to MaxTTL. A nil result means
// unlimited retention. requested is the raw TaskMeta.TTL — nil when the
// requestor expressed no preference.
func (l Lifecycle) enforcedTTL(requested *int64) *int64 {
	ms := int64(0)
	switch {
	case requested != nil && *requested > 0:
		ms = *requested
	case l.DefaultTTL > 0:
		ms = l.DefaultTTL.Milliseconds()
	default:
		return nil // no preference and no default — unlimited retention
	}
	if l.MaxTTL > 0 {
		if maxMS := l.MaxTTL.Milliseconds(); ms > maxMS {
			ms = maxMS
		}
	}
	if ms <= 0 {
		return nil
	}
	out := ms
	return &out
}

// purgeSweep is the background TTL purge sweep — a reusable concurrent artifact
// (CLAUDE.md §5, §14): it runs on its own goroutine, reaps expired tasks on a
// fixed interval, honours context cancellation, and shuts down cleanly. One
// sweep belongs to one Engine.
type purgeSweep struct {
	store    TaskStore
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time // injectable clock; nil uses time.Now
	run      func(context.Context, time.Time) (int, error)

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// newPurgeSweep constructs a purge sweep over store. A non-positive interval
// yields a nil sweep — the caller treats nil as "no sweep configured".
func newPurgeSweep(store TaskStore, interval time.Duration, log *slog.Logger) *purgeSweep {
	if interval <= 0 || store == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &purgeSweep{
		store:    store,
		interval: interval,
		log:      log,
		run:      store.PurgeExpired,
		done:     make(chan struct{}),
	}
}

// nowFn returns the sweep's clock — time.Now unless a test injected one.
func (p *purgeSweep) nowFn() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// Start launches the sweep goroutine bound to ctx. It is idempotent — a second
// call is a no-op. The goroutine stops when ctx is cancelled or Stop is called,
// whichever is first.
func (p *purgeSweep) Start(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stopped {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.started = true
	p.cancel = cancel
	go p.loop(runCtx)
}

// loop runs the sweep until its context is cancelled. It always closes done on
// exit so Stop can join it.
func (p *purgeSweep) loop(ctx context.Context) {
	defer close(p.done)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx)
		}
	}
}

// runOnce performs a single purge pass. A purge error is logged, never
// panicked — the sweep is best-effort and must survive a transient store error
// to run again on the next tick.
func (p *purgeSweep) runOnce(ctx context.Context) {
	n, err := p.run(ctx, p.nowFn())
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down — not a real error
		}
		p.log.ErrorContext(ctx, "task TTL purge sweep failed", slog.String("error", err.Error()))
		return
	}
	if n > 0 {
		p.log.InfoContext(ctx, "task TTL purge sweep reaped expired tasks", slog.Int("count", n))
	}
}

// Stop cancels the sweep and blocks until its goroutine has exited. It is
// idempotent and safe to call even if Start was never called. Once started,
// every concurrent caller waits for the same sweep goroutine to exit. Stop on
// a nil sweep is a no-op.
func (p *purgeSweep) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	started := p.started
	var cancel context.CancelFunc
	if !p.stopped {
		p.stopped = true
		cancel = p.cancel
		if !started {
			close(p.done)
		}
	}
	done := p.done
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		<-done
	}
}
