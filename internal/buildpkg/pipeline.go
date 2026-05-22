package buildpkg

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/validate"
)

// regenerateContracts runs the Design A codegen pipeline over the project so
// the contract artifacts the binary embeds are current (P1, RFC §6). It is the
// first build stage: a build must not ship stale generated output.
func regenerateContracts(ctx context.Context, projectDir string, logger *slog.Logger) error {
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		return fmt.Errorf("%w: load manifest: %w", ErrBuild, err)
	}
	logger.InfoContext(ctx, "build: regenerating contracts")
	if _, err := generate.Run(generate.Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		return fmt.Errorf("%w: regenerate contracts: %w", ErrBuild, err)
	}
	return nil
}

// runValidateGate runs the `dockyard validate` quality gate over the project.
// A validation BLOCKER fails the build — this is where P1 is enforced at build
// time (RFC §9.4, §14). A warning is reported but does not fail the build.
//
// runValidateGate returns ErrValidationBlocked (wrapped) when the report
// carries any blocker, so the caller can branch with errors.Is.
func runValidateGate(ctx context.Context, projectDir string, logger *slog.Logger) error {
	logger.InfoContext(ctx, "build: running validate gate")
	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		return fmt.Errorf("%w: validate could not run: %w", ErrBuild, err)
	}
	if report.HasBlockers() {
		blockers := report.Blockers()
		for _, d := range blockers {
			logger.ErrorContext(ctx, "build: validation blocker", slog.String("diagnostic", d.String()))
		}
		return fmt.Errorf("%w: %d build blocker(s) — fix them and rebuild",
			ErrValidationBlocked, len(blockers))
	}
	if warns := report.Warnings(); len(warns) > 0 {
		logger.WarnContext(ctx, "build: validate passed with warnings", slog.Int("warnings", len(warns)))
	}
	return nil
}

// compileTarget runs `go build` for one cross-compile target and returns the
// produced artifact's absolute path. CGO_ENABLED=0 is pinned so the artifact
// is CGo-free and statically linked (RFC §14, brief 06 §4 R7); GOOS/GOARCH
// select the target. The build runs in the project directory so the project's
// own //go:embed directives resolve against its dist/ tree.
//
// The output name is "<project-name>-<os>-<arch>[.exe]" so a dist/ tree of the
// whole matrix is unambiguous.
func compileTarget(ctx context.Context, projectDir, outputDir, binBase string, t Target) (string, error) {
	if err := t.validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil { //nolint:gosec // a build output dir is browsable, not a secret
		return "", fmt.Errorf("%w: create output dir %s: %w", ErrBuild, outputDir, err)
	}
	artifactName := fmt.Sprintf("%s-%s-%s%s", binBase, t.OS, t.Arch, t.binarySuffix())
	artifactPath := filepath.Join(outputDir, artifactName)

	// -ldflags='-s -w' strips the symbol table and DWARF — the same flags
	// `make build` uses for the dockyard CLI itself. The build target is the
	// project root: a Dockyard project's main package is at its root (RFC §10).
	cmd := exec.CommandContext(ctx, "go", "build", //nolint:gosec // a fixed `go build` of the caller-supplied project dir
		"-ldflags=-s -w", "-o", artifactPath, ".")
	cmd.Dir = projectDir
	// CGO_ENABLED=0 is the non-negotiable shipped-artifact guarantee
	// (CLAUDE.md §5/§6). GOOS/GOARCH cross-compile to the target triple.
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+t.OS,
		"GOARCH="+t.Arch,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: go build %s: %w\n%s", ErrBuild, t, err, out.String())
	}
	return artifactPath, nil
}
