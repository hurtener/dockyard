package server

import (
	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// Keep the opt-in delegation token (authz.ExposeRawToken) request-scoped: strip
// it when the Tasks engine detaches an async task run from its request context,
// so a task handler never inherits a token that has outlived its request. Wired
// here — runtime/server is the composition layer that imports both authz and
// tasks; runtime/tasks stays auth-agnostic (D-201).
func init() {
	tasks.RegisterDetachScrubber(authz.WithoutRawToken)
}
