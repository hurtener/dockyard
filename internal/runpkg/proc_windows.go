//go:build windows

package runpkg

import "os/exec"

// The shipped binary cross-compiles to Windows, so these stubs keep runpkg
// buildable there. Without a job object the child is signalled directly; a
// proper Windows job-object teardown is a later refinement (mirrors the
// internal/devloop Windows stubs).

// setProcessGroup is a no-op on Windows; process groups are not used.
func setProcessGroup(_ *exec.Cmd) {}

// terminateGroup asks the child to exit. Without a job object this signals the
// process directly.
func terminateGroup(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	_ = c.Process.Kill()
}

// killGroup force-kills the child.
func killGroup(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	_ = c.Process.Kill()
}
