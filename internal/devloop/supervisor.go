package devloop

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// stopGrace is how long a supervised child is given to exit after a SIGTERM
// before it is force-killed. A Go MCP server over stdio shuts down promptly;
// the grace window covers a slow flush, not an indefinite hang.
const stopGrace = 3 * time.Second

// command describes one child process the supervisor runs. It is a value type
// so a restart builds a fresh *exec.Cmd from the same description — an
// *exec.Cmd is single-use.
type command struct {
	// name is the human label used in log lines ("go server", "vite").
	name string
	// path is the executable; args are its arguments.
	path string
	args []string
	// dir is the working directory the child runs in.
	dir string
	// env is the child environment; nil inherits the parent's.
	env []string
}

// supervisor owns the lifecycle of one child process: start, restart, and a
// clean stop. It is concurrency-safe — Start, Restart and Stop may be called
// from different goroutines — and it never leaks a process: every started
// child is either still tracked or has been reaped.
//
// A supervisor is single-purpose: one supervisor per child role (the Go
// server, Vite). The orchestrator holds a slice of them.
type supervisor struct {
	cmd    command
	logger *slog.Logger
	// ctx is the dev session's context, used only so the supervisor's log
	// calls can use the context-aware slog API (the repo logging convention).
	// It does NOT drive the child lifecycle — Stop is the single teardown path.
	ctx context.Context //nolint:containedctx // logging context for a per-run object

	mu      sync.Mutex
	current *exec.Cmd
	// done is closed when the currently-tracked child has been reaped. A nil
	// done means no child is running.
	done chan struct{}
	// stopped is set once Stop has run; a stopped supervisor refuses Start and
	// Restart so a late watcher event cannot resurrect a torn-down tree.
	stopped bool
	// onExit, when set, is called once with the child's wait error each time a
	// tracked child exits on its own (not via Stop/Restart). It lets the
	// orchestrator report a crash without the supervisor knowing the policy.
	onExit func(error)
}

// newSupervisor builds a supervisor for one child command. ctx is used only
// for context-aware logging; the child lifecycle is driven by Start/Stop.
func newSupervisor(ctx context.Context, cmd command, logger *slog.Logger) *supervisor {
	return &supervisor{ctx: ctx, cmd: cmd, logger: logger}
}

// Start launches the child process. It is an error to Start a supervisor that
// already has a running child or has been stopped.
//
// The supervisor's lifecycle is driven by explicit Start/Restart/Stop, not by
// a context: a context-killed child (exec.CommandContext) would race the
// orchestrator's own teardown ordering. The orchestrator cancels its watcher
// context and then calls Stop — Stop is the single, ordered teardown path.
func (s *supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

// startLocked launches the child; the caller holds s.mu.
func (s *supervisor) startLocked() error {
	if s.stopped {
		return fmt.Errorf("devloop: supervisor %q is stopped", s.cmd.name)
	}
	if s.current != nil {
		return fmt.Errorf("devloop: supervisor %q already running", s.cmd.name)
	}

	c := exec.Command(s.cmd.path, s.cmd.args...) //nolint:gosec // command is a Dockyard-composed dev command
	c.Dir = s.cmd.dir
	c.Env = s.cmd.env
	// Inherited stdio: the child's logs stream straight to the developer's
	// terminal alongside dev's own log/slog output. devloop does not capture
	// or proxy them — it is an orchestrator, not a log aggregator.
	wireChildStdio(c)
	// Start the child in its own process group so Stop can signal the whole
	// group — a `go run` child itself spawns the compiled binary, and only a
	// group-wide signal reaps that grandchild without an orphan.
	setProcessGroup(c)

	if err := c.Start(); err != nil {
		return fmt.Errorf("devloop: start %q: %w", s.cmd.name, err)
	}
	s.current = c
	done := make(chan struct{})
	s.done = done
	s.logger.InfoContext(s.ctx, "started", slog.String("child", s.cmd.name), slog.Int("pid", c.Process.Pid))

	// One reaper goroutine per child generation. It blocks on Wait, closes
	// done, and — if the exit was not orchestrator-driven — reports it via
	// onExit. The goroutine always terminates when the child does, so a
	// restart or a Stop does not leak goroutines.
	go s.reap(c, done)
	return nil
}

// reap waits for one child generation to exit and closes its done channel.
func (s *supervisor) reap(c *exec.Cmd, done chan struct{}) {
	err := c.Wait()

	s.mu.Lock()
	// Only react if this is still the tracked generation. A Restart/Stop that
	// already swapped s.current has taken ownership of the exit itself.
	isTracked := s.current == c
	if isTracked {
		s.current = nil
		s.done = nil
	}
	onExit := s.onExit
	s.mu.Unlock()

	close(done)
	if isTracked && onExit != nil {
		onExit(err)
	}
}

// Restart stops the current child (if any) and starts a fresh one. It is the
// developer-visible behaviour on a .go change. A restart of a stopped
// supervisor is an error.
func (s *supervisor) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return fmt.Errorf("devloop: supervisor %q is stopped", s.cmd.name)
	}
	s.stopChildLocked()
	return s.startLocked()
}

// Stop terminates the child and marks the supervisor stopped. It is idempotent
// and safe to call even if the child already exited on its own. After Stop the
// supervisor refuses Start and Restart.
func (s *supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.stopChildLocked()
}

// stopChildLocked terminates the tracked child and waits for the reaper to
// finish; the caller holds s.mu. It signals SIGTERM, waits stopGrace, then
// SIGKILLs a child that has not exited — so no orphan survives. The child is
// detached from tracking before signalling so its reaper does not re-enter
// onExit for an orchestrator-driven stop.
func (s *supervisor) stopChildLocked() {
	c := s.current
	done := s.done
	if c == nil {
		return
	}
	s.current = nil
	s.done = nil

	terminateGroup(c)

	// Release the lock while waiting for the reaper: the reaper takes s.mu.
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(stopGrace):
		killGroup(c)
		<-done
	}
	s.mu.Lock()
	s.logger.InfoContext(s.ctx, "stopped", slog.String("child", s.cmd.name))
}

// running reports whether a child is currently tracked. Test-facing.
func (s *supervisor) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current != nil
}

// pid returns the tracked child's PID, or 0 if none. Test-facing.
func (s *supervisor) pid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return 0
	}
	return s.current.Process.Pid
}
