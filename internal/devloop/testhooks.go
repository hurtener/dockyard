package devloop

// This file exposes the orchestrator's lifecycle-observation seam to
// out-of-package tests — chiefly the Phase 19 integration test in
// test/integration. The seam is test-only: it carries no production behaviour,
// it only lets a test wait on a deterministic signal (a restart happened, a
// codegen run finished) instead of sleeping. Run ignores a nil hooks field, so
// production `dockyard dev` pays nothing for it.
//
// It lives in a regular (non-_test.go) file because a _test.go file's symbols
// are not importable from another package; the cost is one tiny exported
// surface, which is the standard Go idiom for a cross-package test seam.

// TestHooks is the set of lifecycle callbacks an out-of-package test attaches
// to a dev-orchestrator run. Every field is optional; a nil callback is not
// invoked.
type TestHooks struct {
	// OnReady fires once, after the initial process tree is up and the watcher
	// is running.
	OnReady func()
	// OnServerRestart fires after each successful Go-server (re)start triggered
	// by a file change.
	OnServerRestart func()
	// OnCodegen fires after each in-process codegen run with its error (nil on
	// success).
	OnCodegen func(error)
	// OnInspectorReady fires once, after the supervised inspector child has its
	// loopback listener open and is serving. The argument is the resolved
	// inspector URL (e.g. http://127.0.0.1:54321) so the test can drive the
	// inspector directly without polling for a port. When DisableInspector is
	// set (the --no-inspector flag's effect), the hook never fires. Added by
	// v1.1 Wave A (D-161) to make the auto-attach observable from an
	// out-of-package integration test.
	OnInspectorReady func(url string)
}

// WithTestHooks returns a copy of cfg with the given test hooks attached. It is
// the only way to populate the orchestrator's unexported hooks field from
// another package. Production callers never use it.
func WithTestHooks(cfg Config, h TestHooks) Config {
	cfg.hooks = &hooks{
		onReady:          h.OnReady,
		onServerRestart:  h.OnServerRestart,
		onCodegen:        h.OnCodegen,
		onInspectorReady: h.OnInspectorReady,
	}
	return cfg
}
