package buildpkg

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// webDir is the project-relative directory holding the Vite UI project. A
// no-template blank server has none; an App project's UI lives here.
const webDir = "web"

// detectViteProject reports whether projectDir has a Vite UI project to build,
// and the absolute path to it. The signal is a web/package.json — the Vite
// project is a real npm project and a package.json is its root. This mirrors
// devloop.detectViteProject so `dockyard build` and `dockyard dev` agree on
// what a UI project is.
//
// A blank server (the no-template `dockyard new`) has no web/ directory; the
// build degrades gracefully and skips the Vite step (RFC §4.1: a UI resource
// is additive).
func detectViteProject(projectDir string) (dir string, found bool) {
	web := filepath.Join(projectDir, webDir)
	pkg := filepath.Join(web, "package.json")
	if info, err := os.Stat(pkg); err == nil && !info.IsDir() {
		return web, true
	}
	return "", false
}

// buildViteUI builds the project's web/ Vite UI so the dist/ embed target
// exists on disk before `go build` reads the //go:embed directive (RFC §14,
// the embed ordering). When the project has no web/ UI it returns (false, nil)
// — a clean, logged graceful skip, not an error.
//
// The build invocation is `npm run build`, the convention a Vite-scaffolded
// web project ships in its package.json scripts; npm must be installed and the
// web/ project's dependencies present (a release pipeline runs `npm ci`
// first). A missing npm or a Vite build failure is a clear, typed error — the
// build cannot embed a UI it could not produce.
func buildViteUI(ctx context.Context, projectDir string, logger *slog.Logger) (built bool, err error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	web, found := detectViteProject(projectDir)
	if !found {
		logger.InfoContext(ctx, "build: no web/ UI project — skipping Vite step")
		return false, nil
	}
	if _, lookErr := exec.LookPath("npm"); lookErr != nil {
		return false, fmt.Errorf(
			"%w: project has a web/ UI but npm is not installed — install Node.js to build the UI: %w",
			ErrBuild, lookErr)
	}
	logger.InfoContext(ctx, "build: building web/ UI with Vite", slog.String("dir", web))

	cmd := exec.CommandContext(ctx, "npm", "run", "build")
	cmd.Dir = web
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if runErr := cmd.Run(); runErr != nil {
		return false, fmt.Errorf("%w: vite build failed: %w\n%s",
			ErrBuild, runErr, out.String())
	}
	return true, nil
}
