package devloop

import (
	"os"
	"path/filepath"
)

// webDir is the project-relative directory holding the Vite UI project. A
// scaffolded blank server has none; an App project's UI lives here.
const webDir = "web"

// detectViteProject reports whether projectDir has a Vite UI project to
// supervise, and the absolute path to it. The signal is a web/package.json:
// the Vite project is a real npm project and a package.json is its root.
//
// A blank server (the no-template `dockyard new`) has no web/ directory; in
// that case devloop degrades gracefully and supervises only the Go server
// (RFC §4.1: a UI resource is additive). Detection is a pure filesystem check
// so the caller can log the degradation cleanly.
func detectViteProject(projectDir string) (dir string, found bool) {
	web := filepath.Join(projectDir, webDir)
	pkg := filepath.Join(web, "package.json")
	if info, err := os.Stat(pkg); err == nil && !info.IsDir() {
		return web, true
	}
	return "", false
}

// viteCommand builds the command that runs the project's Vite dev server.
// Vite owns Svelte HMR (RFC §9.2, brief 06 §2.6) — devloop starts and
// supervises it, it never reimplements hot reload. The default invocation is
// `npm run dev`, the convention a Vite-scaffolded web project ships in its
// package.json scripts.
//
// override is the test/seam injection point: an injected command stands in for
// `npm run dev` so the integration test does not need a real Node toolchain.
func viteCommand(webProjectDir string, override []string) command {
	c := command{
		name: "vite",
		dir:  webProjectDir,
		env:  os.Environ(),
	}
	if len(override) > 0 {
		c.path = override[0]
		c.args = override[1:]
		return c
	}
	c.path = "npm"
	c.args = []string{"run", "dev"}
	return c
}
