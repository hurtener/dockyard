package devloop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// childProgram is a tiny self-contained Go program used as a controllable
// supervised child in tests. Behaviour is selected by os.Args[1]:
//
//	"run"   — block forever (until signalled); the normal long-running case.
//	"crash" — exit non-zero immediately; the crash/failure-mode case.
//	"touch" — create a file named by os.Args[2], then block forever; lets a
//	          test observe that a (re)start actually happened.
const childProgram = `package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	mode := "run"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "crash":
		os.Exit(3)
	case "touch":
		if len(os.Args) > 2 {
			f, err := os.OpenFile(os.Args[2], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err == nil {
				_, _ = f.WriteString("x")
				_ = f.Close()
			}
		}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
}
`

// childBinPath compiles childProgram once per test process and returns the
// path to the built binary. Compiling a real binary (rather than `go run`)
// keeps each supervised start fast and the process tree shallow — important
// for the no-orphan assertions.
var (
	childBinOnce sync.Once
	childBin     string
	childBinErr  error
)

func buildChildBin(t *testing.T) string {
	t.Helper()
	childBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "devloop-child-*")
		if err != nil {
			childBinErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(childProgram), 0o600); err != nil {
			childBinErr = err
			return
		}
		bin := filepath.Join(dir, "child")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, src) //nolint:gosec // bin/src are test temp paths
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			childBinErr = err
			t.Logf("child build output: %s", out)
			return
		}
		childBin = bin
	})
	if childBinErr != nil {
		t.Fatalf("build child binary: %v", childBinErr)
	}
	return childBin
}

// processAlive reports whether a PID names a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 is the liveness probe on POSIX.
	return p.Signal(syscallSignal0) == nil
}

func TestSupervisorStartStop(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"run"}}, quietLogger())

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !s.running() {
		t.Fatal("supervisor reports not running after Start")
	}
	pid := s.pid()
	if !processAlive(pid) {
		t.Fatalf("child pid %d is not alive after Start", pid)
	}

	s.Stop()
	if s.running() {
		t.Fatal("supervisor still running after Stop")
	}
	// The child must be reaped — no orphan process.
	waitDead(t, pid)
}

func TestSupervisorRestartReplacesChild(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"run"}}, quietLogger())
	t.Cleanup(s.Stop)

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	first := s.pid()

	if err := s.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	second := s.pid()

	if first == second {
		t.Fatalf("restart did not replace the child: pid %d unchanged", first)
	}
	// The old generation must be gone — restart is terminate-then-start.
	waitDead(t, first)
	if !processAlive(second) {
		t.Fatalf("new child pid %d not alive after restart", second)
	}
}

func TestSupervisorStopIsIdempotent(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"run"}}, quietLogger())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	s.Stop() // second Stop must not panic or block
}

func TestSupervisorRejectsStartAfterStop(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"run"}}, quietLogger())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	// A late watcher event must not resurrect a torn-down tree.
	if err := s.Start(); err == nil {
		t.Fatal("Start succeeded on a stopped supervisor")
	}
	if err := s.Restart(); err == nil {
		t.Fatal("Restart succeeded on a stopped supervisor")
	}
}

func TestSupervisorReportsCrash(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	crashed := make(chan error, 1)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"crash"}}, quietLogger())
	s.onExit = func(err error) { crashed <- err }
	t.Cleanup(s.Stop)

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The wait returns the instant onExit fires; the ceiling only guards
	// against a genuine hang. It is generous (not the 3s it once was) so a
	// CPU-saturated CI run — many -race packages compiling and testing in
	// parallel — cannot starve the child spawn/exit/Wait sequence past the
	// deadline and produce a spurious failure. A real never-fires bug still
	// fails, just 10s later.
	select {
	case err := <-crashed:
		if err == nil {
			t.Fatal("onExit fired with nil error for a crashing child")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("onExit never fired for a crashing child")
	}
}

func TestSupervisorStartFailsForMissingBinary(t *testing.T) {
	t.Parallel()
	s := newSupervisor(context.Background(), command{name: "test", path: "/nonexistent/dockyard-devloop-missing"}, quietLogger())
	if err := s.Start(); err == nil {
		t.Fatal("Start succeeded for a missing binary")
	}
	if s.running() {
		t.Fatal("supervisor reports running after a failed Start")
	}
}

// TestSupervisorConcurrentRestart drives Restart from many goroutines while
// the child is running — the -race proof that the supervisor is safe under
// concurrent use and never leaks a process.
func TestSupervisorConcurrentRestart(t *testing.T) {
	t.Parallel()
	bin := buildChildBin(t)
	s := newSupervisor(context.Background(), command{name: "test", path: bin, args: []string{"run"}}, quietLogger())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = s.Restart()
			}
		}()
	}
	wg.Wait()

	last := s.pid()
	s.Stop()
	waitDead(t, last)
}

// waitDead fails the test if pid is still alive after a bounded wait.
func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process pid %d is still alive — orphan leak", pid)
}
