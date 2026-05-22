package devloop

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// quietLogger is a slog.Logger that discards output — test runs stay quiet.
func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestChangeKindString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind changeKind
		want string
	}{
		{changeNone, "none"},
		{changeGo, "go-source"},
		{changeContract, "contract-source"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("changeKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestMergeKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b, want changeKind
	}{
		{changeNone, changeNone, changeNone},
		{changeGo, changeNone, changeGo},
		{changeNone, changeGo, changeGo},
		{changeGo, changeGo, changeGo},
		{changeContract, changeGo, changeContract},
		{changeGo, changeContract, changeContract},
		{changeContract, changeContract, changeContract},
		// A contract change subsumes a plain .go change: codegen must run.
		{changeContract, changeNone, changeContract},
	}
	for _, c := range cases {
		if got := mergeKind(c.a, c.b); got != c.want {
			t.Errorf("mergeKind(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestIsIgnoredDir(t *testing.T) {
	t.Parallel()
	ignored := []string{".git", "node_modules", "dist", "build", ".svelte-kit", "vendor", ".dockyard-gen-123"}
	for _, d := range ignored {
		if !isIgnoredDir(d) {
			t.Errorf("isIgnoredDir(%q) = false, want true", d)
		}
	}
	watched := []string{"internal", "contracts", "web", "src", "cmd"}
	for _, d := range watched {
		if isIgnoredDir(d) {
			t.Errorf("isIgnoredDir(%q) = true, want false", d)
		}
	}
}

// newTestWatcher builds a watcher over a fresh temp project tree with a
// contracts directory, ready for event-classification tests.
func newTestWatcher(t *testing.T) (*watcher, string) {
	t.Helper()
	dir := t.TempDir()
	contracts := filepath.Join(dir, "internal", "contracts")
	if err := os.MkdirAll(contracts, 0o750); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	w, err := newWatcher(dir, "internal/contracts", 50*time.Millisecond, quietLogger())
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.close() })
	return w, dir
}

func TestWatcherClassify(t *testing.T) {
	t.Parallel()
	w, dir := newTestWatcher(t)

	cases := []struct {
		name string
		path string
		op   fsnotify.Op
		want changeKind
	}{
		{"plain go write", filepath.Join(dir, "main.go"), fsnotify.Write, changeGo},
		{"contract go write", filepath.Join(dir, "internal", "contracts", "contracts.go"), fsnotify.Write, changeContract},
		{"non-go file", filepath.Join(dir, "README.md"), fsnotify.Write, changeNone},
		{"chmod ignored", filepath.Join(dir, "main.go"), fsnotify.Chmod, changeNone},
		{"go create", filepath.Join(dir, "tool.go"), fsnotify.Create, changeGo},
		{"go remove", filepath.Join(dir, "old.go"), fsnotify.Remove, changeGo},
		{"test file is go change", filepath.Join(dir, "main_test.go"), fsnotify.Write, changeGo},
		{"yaml manifest ignored", filepath.Join(dir, "dockyard.app.yaml"), fsnotify.Write, changeNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := w.classify(fsnotify.Event{Name: c.path, Op: c.op})
			if got != c.want {
				t.Errorf("classify(%s, %v) = %v, want %v", c.path, c.op, got, c.want)
			}
		})
	}
}

// TestWatcherDebounceCoalescesBurst proves a burst of file events within the
// debounce window produces exactly one change event — not one per write.
//
// The burst is fed straight into the debounce engine (w.loop) through a
// channel the test owns, so coalescing is proven with no dependence on
// OS/fsnotify event-delivery timing — every synthetic event reaches the
// debouncer before the window opens, on a quiet machine or a saturated CI
// runner alike.
func TestWatcherDebounceCoalescesBurst(t *testing.T) {
	t.Parallel()
	w, dir := newTestWatcher(t)
	// A deliberately large debounce window: the burst is fed synchronously
	// below, and a window this wide leaves no room for a scheduler stall
	// between two channel sends to outlast it, even on a saturated runner.
	w.debounce = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan fsnotify.Event)
	errs := make(chan error)
	go w.loop(ctx, events, errs)

	goFile := filepath.Join(dir, "main.go")
	// A burst of ten writes. Sends on the unbuffered channel are synchronous:
	// loop has consumed each event (and reset the debounce timer) before the
	// next is sent, so the whole burst lands inside one debounce window
	// regardless of scheduler jitter.
	for i := 0; i < 10; i++ {
		events <- fsnotify.Event{Name: goFile, Op: fsnotify.Write}
	}

	// Exactly one coalesced event should arrive — one debounce window after
	// the final write, with a generous margin for the timer to fire.
	select {
	case kind := <-w.events:
		if kind != changeGo {
			t.Fatalf("first coalesced event = %v, want changeGo", kind)
		}
	case <-time.After(w.debounce + 3*time.Second):
		t.Fatal("no coalesced event after a burst")
	}

	// No second event should follow the single burst. A missed coalesce would
	// have flushed a second event one debounce window after some later write;
	// waiting past a full window with margin proves none is pending.
	select {
	case kind := <-w.events:
		t.Fatalf("unexpected second event %v — burst was not coalesced", kind)
	case <-time.After(w.debounce + time.Second):
	}
}

// TestWatcherContractChangeWins proves a burst touching both a contract file
// and a plain .go file is classified contract-source (codegen must run).
//
// Like TestWatcherDebounceCoalescesBurst, the burst is fed directly into the
// debounce engine so the single-classified-outcome assertion holds without a
// wall-clock dependency on fsnotify delivery timing.
func TestWatcherContractChangeWins(t *testing.T) {
	t.Parallel()
	w, dir := newTestWatcher(t)
	// A large debounce window: the two events below are fed synchronously, so
	// a window this wide leaves no room for a scheduler stall between the
	// sends to outlast it.
	w.debounce = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan fsnotify.Event)
	errs := make(chan error)
	go w.loop(ctx, events, errs)

	// Synchronous sends on the unbuffered channel guarantee both events land
	// inside one debounce window.
	events <- fsnotify.Event{Name: filepath.Join(dir, "main.go"), Op: fsnotify.Write}
	contractFile := filepath.Join(dir, "internal", "contracts", "contracts.go")
	events <- fsnotify.Event{Name: contractFile, Op: fsnotify.Write}

	select {
	case kind := <-w.events:
		if kind != changeContract {
			t.Fatalf("coalesced kind = %v, want changeContract", kind)
		}
	case <-time.After(w.debounce + 3*time.Second):
		t.Fatal("no coalesced event")
	}
}

// TestWatcherStopsOnContextCancel proves the watch loop exits and closes its
// events channel when its context is cancelled — no leaked goroutine.
func TestWatcherStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	w, _ := newTestWatcher(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher.run did not return after context cancel")
	}
	// The events channel must be closed on exit.
	if _, ok := <-w.events; ok {
		t.Fatal("events channel was not closed on watcher exit")
	}
}

// TestWatcherIgnoresNoiseDir proves a write inside an ignored directory
// (node_modules) produces no change event.
func TestWatcherIgnoresNoiseDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	noise := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(noise, 0o750); err != nil {
		t.Fatalf("mkdir noise: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "contracts"), 0o750); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	w, err := newWatcher(dir, "internal/contracts", 50*time.Millisecond, quietLogger())
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	defer func() { _ = w.close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx)

	// A .go write inside node_modules is not watched at all.
	if err := os.WriteFile(filepath.Join(noise, "index.go"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write noise file: %v", err)
	}
	select {
	case kind := <-w.events:
		t.Fatalf("got change %v from an ignored directory", kind)
	case <-time.After(400 * time.Millisecond):
	}
}
