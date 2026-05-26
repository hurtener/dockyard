package scaffold

import "github.com/hurtener/dockyard/internal/manifest"

// RequiresTasksEngine reports whether any tool in m declares
// task_support: optional or required — the manifest-side signal that a
// project's main.go must construct a tasks.Engine and attach it via
// server.Options{Tasks: engine}.
//
// It is the detection seam D-164 closes: the scaffold consults it at
// generation time to decide whether to emit the engine-wired main.go
// template, and `dockyard run` consults it at run time to surface a
// warning when the manifest declares task support but the project's
// main.go source does not appear to attach the engine.
//
// A nil manifest, an empty Tools slice, and a Tools slice in which
// every tool declares task_support: forbidden (or the empty zero value,
// which the loader normalises to forbidden — RFC §8.4) all yield false.
// One tool with task_support: optional or required is enough to yield
// true; the engine attaches once per server, not per tool.
func RequiresTasksEngine(m *manifest.Manifest) bool {
	if m == nil {
		return false
	}
	for _, t := range m.Tools {
		switch t.TaskSupport {
		case manifest.TaskSupportOptional, manifest.TaskSupportRequired:
			return true
		}
	}
	return false
}
