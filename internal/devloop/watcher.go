package devloop

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultDebounce is the window a burst of file events is coalesced into one
// trigger. An editor save, a `gofmt`, and a VCS checkout each fire several
// fsnotify events in quick succession; debouncing turns that burst into a
// single rebuild rather than a thundering restart.
const defaultDebounce = 250 * time.Millisecond

// changeKind classifies a coalesced file-change burst into the orchestrator's
// reaction. A burst that touches both a contract file and a plain .go file is
// reported as changeContract so codegen runs before the restart (the
// regenerate-then-restart ordering of RFC §9.2).
type changeKind int

const (
	// changeNone is the zero value — no actionable change in the burst.
	changeNone changeKind = iota
	// changeGo is a plain .go source change ⇒ rebuild + restart the server.
	changeGo
	// changeContract is a change under the contracts directory ⇒ regenerate,
	// then rebuild + restart the server.
	changeContract
)

func (k changeKind) String() string {
	switch k {
	case changeGo:
		return "go-source"
	case changeContract:
		return "contract-source"
	default:
		return "none"
	}
}

// watcher embeds an fsnotify recursive watch over a project tree and emits a
// debounced, classified changeKind on every actionable burst. It is the
// "Dockyard does not shell out to air/wgo" half of RFC §9.2.
type watcher struct {
	projectDir   string
	contractsDir string // absolute path of the project's contracts directory
	debounce     time.Duration
	logger       *slog.Logger

	fsw *fsnotify.Watcher
	// events is the orchestrator-facing stream of coalesced changes.
	events chan changeKind
}

// newWatcher builds a watcher rooted at projectDir. contractsRel is the
// project-relative contracts directory (internal/generate.ContractsDir) so a
// change there is classified as changeContract.
func newWatcher(projectDir, contractsRel string, debounce time.Duration, logger *slog.Logger) (*watcher, error) {
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("devloop: create fsnotify watcher: %w", err)
	}
	w := &watcher{
		projectDir:   projectDir,
		contractsDir: filepath.Join(projectDir, filepath.FromSlash(contractsRel)),
		debounce:     debounce,
		logger:       logger,
		fsw:          fsw,
		events:       make(chan changeKind, 1),
	}
	if err := w.addTree(); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	return w, nil
}

// addTree registers every directory under the project root with fsnotify.
// fsnotify watches directories, not trees, so each directory is added
// explicitly. Noise directories (the VCS dir, node_modules, build output) are
// skipped — watching node_modules would drown the loop in events.
func (w *watcher) addTree() error {
	return filepath.WalkDir(w.projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if isIgnoredDir(d.Name()) && path != w.projectDir {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(path); err != nil {
			return fmt.Errorf("devloop: watch %s: %w", path, err)
		}
		return nil
	})
}

// isIgnoredDir reports whether a directory is dev-loop noise that must not be
// watched. node_modules and build output produce torrents of events; the VCS
// dir and dotdirs are not source.
func isIgnoredDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", ".svelte-kit", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".dockyard-gen-")
}

// run drives the watch loop until ctx is cancelled. It coalesces fsnotify
// events over the debounce window and sends one classified changeKind per
// burst on w.events. run owns the debounce timer; it never blocks the
// orchestrator — a send on the buffered events channel that would block is
// dropped because a rebuild is already pending (coalescing across the
// channel boundary too).
//
// run is a thin adapter that binds the real fsnotify channels to loop, the
// transport-agnostic debounce engine. The split lets the debounce/coalesce
// logic be exercised deterministically by feeding synthetic events into loop
// without a real fsnotify watcher or real files.
func (w *watcher) run(ctx context.Context) {
	w.loop(ctx, w.fsw.Events, w.fsw.Errors)
}

// loop is the transport-agnostic debounce engine. It coalesces events from the
// events channel over the debounce window, emitting one classified changeKind
// per burst on w.events, and survives watch errors from errs. It returns —
// closing w.events — when ctx is cancelled or either input channel closes.
//
// run wires loop to the real fsnotify channels in production; tests drive loop
// directly with channels they control, so coalescing is proven with no
// dependence on OS/fsnotify event-delivery timing.
func (w *watcher) loop(ctx context.Context, events <-chan fsnotify.Event, errs <-chan error) {
	defer close(w.events)

	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	pending := changeNone

	for {
		select {
		case <-ctx.Done():
			timer.Stop()
			return

		case ev, ok := <-events:
			if !ok {
				timer.Stop()
				return
			}
			kind := w.classify(ev)
			if kind == changeNone {
				continue
			}
			// A newly-created directory must be added to the watch set, else
			// edits inside it after `dockyard new`'s tree was scanned go unseen.
			if ev.Op.Has(fsnotify.Create) && isDir(ev.Name) && !isIgnoredDir(filepath.Base(ev.Name)) {
				_ = w.fsw.Add(ev.Name)
			}
			pending = mergeKind(pending, kind)
			timer.Reset(w.debounce)

		case err, ok := <-errs:
			if !ok {
				timer.Stop()
				return
			}
			// A watch error is reported and the loop survives — a dropped
			// inotify event is not fatal to the dev session.
			w.logger.WarnContext(ctx, "file watch error", slog.String("error", err.Error()))

		case <-timer.C:
			if pending == changeNone {
				continue
			}
			w.emit(ctx, pending)
			pending = changeNone
		}
	}
}

// emit sends a coalesced change to the orchestrator. The events channel is
// buffered to 1; if a change is already queued, this one is merged into it by
// the buffer being full — a rebuild is pending either way, so dropping is
// correct coalescing, not lost work.
func (w *watcher) emit(ctx context.Context, kind changeKind) {
	select {
	case w.events <- kind:
		w.logger.InfoContext(ctx, "change detected", slog.String("kind", kind.String()))
	case <-ctx.Done():
	default:
		// A rebuild is already queued; this burst folds into it.
		w.logger.DebugContext(ctx, "change coalesced into pending rebuild", slog.String("kind", kind.String()))
	}
}

// classify maps one fsnotify event to a changeKind. Only Create/Write/Remove/
// Rename on a .go file is actionable; a chmod or a non-.go file is ignored.
// A .go file under the contracts directory is changeContract.
func (w *watcher) classify(ev fsnotify.Event) changeKind {
	if !ev.Op.Has(fsnotify.Create) && !ev.Op.Has(fsnotify.Write) &&
		!ev.Op.Has(fsnotify.Remove) && !ev.Op.Has(fsnotify.Rename) {
		return changeNone
	}
	if filepath.Ext(ev.Name) != ".go" {
		return changeNone
	}
	// A generated-test or generator-temp file is not developer source.
	if strings.HasSuffix(ev.Name, "_test.go") {
		// A test-file edit still warrants a restart so the dev server reflects
		// it; treat it as a plain .go change, not a contract change.
		return changeGo
	}
	if w.underContracts(ev.Name) {
		return changeContract
	}
	return changeGo
}

// underContracts reports whether an absolute path lies within the project's
// contracts directory.
func (w *watcher) underContracts(path string) bool {
	rel, err := filepath.Rel(w.contractsDir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// close releases the fsnotify watcher's OS resources.
func (w *watcher) close() error {
	return w.fsw.Close()
}

// mergeKind combines two change kinds within one debounce burst. A contract
// change subsumes a plain .go change — codegen must run, and codegen is
// followed by a server rebuild anyway.
func mergeKind(a, b changeKind) changeKind {
	if a == changeContract || b == changeContract {
		return changeContract
	}
	if a == changeGo || b == changeGo {
		return changeGo
	}
	return changeNone
}
