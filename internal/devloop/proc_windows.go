//go:build windows

package devloop

import (
	"os"
	"os/exec"
)

// Windows is not a V1 `dockyard dev` target — the dev loop is a
// developer-machine tool and V1 targets POSIX (see phase-19 plan, Risks). The
// shipped binary still cross-compiles, so these stubs keep the package
// buildable on Windows: the child is started and signalled without
// process-group semantics. A proper Windows job-object teardown is a later
// refinement.

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

// wireChildStdio connects the child's streams to the developer's terminal.
func wireChildStdio(c *exec.Cmd) {
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
}

// syscallSignal0 is the liveness-probe signal a test uses. On Windows there is
// no signal 0; os.Process.Signal rejects it, so a test's processAlive probe is
// less precise there — Windows is not a V1 dev-loop target (see phase-19 plan).
var syscallSignal0 os.Signal = os.Kill
