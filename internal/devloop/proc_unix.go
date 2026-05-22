//go:build !windows

package devloop

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup starts the child in its own process group so the whole
// group can be signalled at once. A `go run` child itself forks the compiled
// binary; signalling only the `go run` PID would orphan that grandchild.
// Starting a fresh group and signalling the group (negative PID) reaps the
// whole subtree — no orphan process, no leaked port (CLAUDE.md §13).
func setProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}

// terminateGroup sends SIGTERM to the child's whole process group, asking it
// to shut down cleanly. A failure is best-effort: a child that already exited
// has no group to signal.
func terminateGroup(c *exec.Cmd) {
	signalGroup(c, syscall.SIGTERM)
}

// killGroup force-kills the child's process group — the escalation when a
// SIGTERM grace window elapses without the child exiting.
func killGroup(c *exec.Cmd) {
	signalGroup(c, syscall.SIGKILL)
}

// signalGroup delivers sig to the child's process group. Signalling -pgid
// reaches every process in the group; if the group lookup fails (the child is
// already gone) it falls back to signalling the process directly.
func signalGroup(c *exec.Cmd, sig syscall.Signal) {
	if c == nil || c.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(c.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, sig)
		return
	}
	_ = c.Process.Signal(sig)
}

// wireChildStdio connects the child's stdout/stderr to the developer's
// terminal so the supervised process's own logs stream alongside dev's output.
func wireChildStdio(c *exec.Cmd) {
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
}

// syscallSignal0 is the POSIX "signal 0" — delivered to no handler, it is the
// liveness probe a test uses to ask whether a PID is still a live process.
var syscallSignal0 os.Signal = syscall.Signal(0)
