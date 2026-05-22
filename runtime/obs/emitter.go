package obs

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Emitter is the obs/v1 emit seam. The runtime depends ONLY on this interface;
// the inspector, the SSE sink, and the OTel adapter are drivers behind it
// (CLAUDE.md §4.4). An Emitter is a reusable concurrent artifact: a single
// value is safe for Emit from many goroutines (CLAUDE.md §5).
//
// Emit MUST be non-blocking: an emitter never blocks the runtime on a slow
// consumer (CLAUDE.md §8). A driver that cannot keep up drops events; it must
// not stall the caller. Emit takes a context for cancellation propagation and
// trace correlation, but it returns no error — observability never fails a
// request (P2).
type Emitter interface {
	// Emit records an event. It is non-blocking and never panics. A malformed
	// event is dropped silently — a buggy emit site never crashes a request.
	Emit(ctx context.Context, e Event)
}

// Closer is implemented by an Emitter that holds resources (a driver with a
// background goroutine, an open socket). [Close] closes every driver in a
// [FanOut] that implements it.
type Closer interface {
	// Close releases the emitter's resources. It is idempotent.
	Close() error
}

// Factory constructs an [Emitter] for a driver-specific configuration string.
// It is the obs analogue of store.Factory (RFC §13): a driver registers one via
// [RegisterDriver] in its init block, and [Open] constructs an emitter by name.
type Factory func(cfg string) (Emitter, error)

var (
	driversMu sync.RWMutex
	drivers   = map[string]Factory{}
)

// RegisterDriver registers an emitter-driver factory under name. It is called
// from a driver package's init block (blank-import registration, CLAUDE.md
// §4.4). Registering the same name twice panics — a duplicate registration is a
// programming error caught at process start. The ring-buffer driver registers
// itself under "ringbuffer"; Phase 16's SSE and OTel drivers register under
// their own names behind this same seam.
func RegisterDriver(name string, f Factory) {
	if f == nil {
		panic("dockyard/runtime/obs: RegisterDriver called with a nil factory")
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, dup := drivers[name]; dup {
		panic(fmt.Sprintf("dockyard/runtime/obs: emitter driver %q already registered", name))
	}
	drivers[name] = f
}

// Drivers returns the names of all registered emitter drivers, sorted.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Open constructs an [Emitter] using the named driver. The driver package must
// be imported so its init block has registered the factory. Open returns an
// error if no such driver is registered.
func Open(driver, cfg string) (Emitter, error) {
	driversMu.RLock()
	f, ok := drivers[driver]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("dockyard/runtime/obs: unknown emitter driver %q (registered: %v)", driver, Drivers())
	}
	e, err := f(cfg)
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/obs: open driver %q: %w", driver, err)
	}
	return e, nil
}

// NopEmitter is an [Emitter] that discards every event. It is the safe default
// when a runtime is constructed without observability configured: a subsystem
// can always call Emit without a nil check. The zero value is ready to use.
type NopEmitter struct{}

// Emit discards e.
func (NopEmitter) Emit(context.Context, Event) {}

// FanOut is an [Emitter] that fans an event out to several drivers — the
// bounded fan-out CLAUDE.md §8 mandates. Each driver receives every event; a
// slow driver cannot stall a fast one because every driver's Emit is itself
// non-blocking (the ring buffer drops; the SSE sink, Phase 16, drops). FanOut
// is safe for concurrent use and for concurrent Close.
type FanOut struct {
	drivers []Emitter
}

// NewFanOut returns a [FanOut] over the given drivers. A nil driver is dropped.
// With no drivers it behaves as a [NopEmitter].
func NewFanOut(drivers ...Emitter) *FanOut {
	out := &FanOut{}
	for _, d := range drivers {
		if d != nil {
			out.drivers = append(out.drivers, d)
		}
	}
	return out
}

// Emit forwards e to every driver. It is non-blocking provided every driver's
// Emit is non-blocking, which the Emitter contract requires.
func (f *FanOut) Emit(ctx context.Context, e Event) {
	for _, d := range f.drivers {
		d.Emit(ctx, e)
	}
}

// Close closes every driver that implements [Closer]. It joins errors so one
// failing driver does not mask another.
func (f *FanOut) Close() error {
	var errs []error
	for _, d := range f.drivers {
		if c, ok := d.(Closer); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("dockyard/runtime/obs: %d driver close errors: %v", len(errs), errs)
	}
}
