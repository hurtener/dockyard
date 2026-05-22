//go:build !windows

package runpkg

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the child in its own process group so the whole group
// can be signalled at once — the same discipline internal/devloop uses so no
// grandchild is orphaned (CLAUDE.md §13).
func setProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}

// terminateGroup sends SIGTERM to the child's whole process group, asking it
// to shut down cleanly. A failure is best-effort: a child that already exited
// has no group to signal.
func terminateGroup(c *exec.Cmd) { signalGroup(c, syscall.SIGTERM) }

// killGroup force-kills the child's process group — the escalation when the
// SIGTERM grace window elapses without the child exiting.
func killGroup(c *exec.Cmd) { signalGroup(c, syscall.SIGKILL) }

// signalGroup delivers sig to the child's process group; it falls back to the
// process directly when the group lookup fails (the child is already gone).
func signalGroup(c *exec.Cmd, sig syscall.Signal) {
	if c == nil || c.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(c.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, sig)
		return
	}
	_ = c.Process.Signal(sig)
}
