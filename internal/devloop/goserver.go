package devloop

import (
	"os"
)

// goServerCommand builds the command that runs the scaffolded project's Go MCP
// server. The scaffold (`dockyard new`, Phase 17) places main.go at the project
// root, so `go run .` builds and runs it. Running via `go run` keeps the dev
// loop a single binary with no separate build step to orchestrate — the
// rebuild is implicit in each restart (Go has no in-process hot reload, so the
// process is restarted, brief 06 §2.6).
//
// CGO_ENABLED=0 matches the shipped-artifact guarantee (CLAUDE.md §5); the dev
// child is not the shipped artifact, but keeping it CGo-free avoids a divergent
// dev-vs-build behaviour.
func goServerCommand(projectDir string, override []string) command {
	c := command{
		name: "go server",
		dir:  projectDir,
		env:  append(os.Environ(), "CGO_ENABLED=0"),
	}
	if len(override) > 0 {
		// Test/seam override: an injected command (e.g. a controllable stub)
		// stands in for the real `go run .` so the integration test stays fast.
		c.path = override[0]
		c.args = override[1:]
		return c
	}
	c.path = "go"
	c.args = []string{"run", "."}
	return c
}

// isDir reports whether path is a directory. Used by the watcher to decide
// whether a freshly-created path needs adding to the watch set.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
