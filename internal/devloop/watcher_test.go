package devloop

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// quietLogger is a slog.Logger that discards output — test runs stay quiet.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
func TestWatcherDebounceCoalescesBurst(t *testing.T) {
	t.Parallel()
	w, dir := newTestWatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx)

	goFile := filepath.Join(dir, "main.go")
	// A burst of ten writes well within one 50ms debounce window.
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(goFile, []byte("package main\n"), 0o600); err != nil {
			t.Fatalf("write burst: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Exactly one coalesced event should arrive.
	select {
	case kind := <-w.events:
		if kind != changeGo {
			t.Fatalf("first coalesced event = %v, want changeGo", kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no coalesced event after a burst")
	}

	// No second event should follow the single burst.
	select {
	case kind := <-w.events:
		t.Fatalf("unexpected second event %v — burst was not coalesced", kind)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestWatcherContractChangeWins proves a burst touching both a contract file
// and a plain .go file is classified contract-source (codegen must run).
func TestWatcherContractChangeWins(t *testing.T) {
	t.Parallel()
	w, dir := newTestWatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx)

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	contractFile := filepath.Join(dir, "internal", "contracts", "contracts.go")
	if err := os.WriteFile(contractFile, []byte("package contracts\n"), 0o600); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	select {
	case kind := <-w.events:
		if kind != changeContract {
			t.Fatalf("coalesced kind = %v, want changeContract", kind)
		}
	case <-time.After(2 * time.Second):
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
