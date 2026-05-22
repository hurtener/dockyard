package buildpkg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ErrBuild is the sentinel wrapping a `dockyard build` failure that is not a
// validation blocker — a missing project, an I/O fault, a `go build` or Vite
// failure. Callers branch with errors.Is(err, ErrBuild).
var ErrBuild = errors.New("dockyard/internal/buildpkg: build failed")

// ErrValidationBlocked is the sentinel wrapping a build that failed because
// the `dockyard validate` gate reported a build blocker. It is distinct from
// ErrBuild so a caller (and the integration test's failure-mode assertion)
// can tell a quality-gate stop from a toolchain fault. RFC §14 / P1: a build
// with a validation blocker fails.
var ErrValidationBlocked = errors.New("dockyard/internal/buildpkg: build blocked by validation")

// defaultOutputDir is the project-relative directory `dockyard build` writes
// artifacts into when Options.OutputDir is empty. It matches the project
// scaffold's .gitignore (/dist/).
const defaultOutputDir = "dist"

// Options configures one `dockyard build` invocation.
type Options struct {
	// ProjectDir is the root of the Dockyard project — the directory holding
	// dockyard.app.yaml. Required.
	ProjectDir string
	// OutputDir is where build artifacts and checksum files are written. An
	// empty value defaults to <ProjectDir>/dist.
	OutputDir string
	// Targets is the cross-compile target set. An empty slice builds the host
	// platform only — the fast inner-loop build; pass DefaultMatrix() for the
	// full RFC §14 release matrix.
	Targets []Target
	// SkipValidate disables the validate gate. It is a test seam only —
	// production builds always validate (P1). A normal build leaves it false.
	SkipValidate bool
	// Logger receives the build's structured progress output. A nil Logger
	// falls back to a discarding logger so Build never panics on a missing
	// logger; a caller should pass the dev-mode text handler.
	Logger *slog.Logger
}

// Artifact is one produced binary and its checksum sidecar.
type Artifact struct {
	// Target is the GOOS/GOARCH this artifact was built for.
	Target Target
	// Path is the absolute path of the built binary.
	Path string
	// ChecksumPath is the absolute path of the binary's .sha256 sidecar.
	ChecksumPath string
}

// Result reports what a Build produced.
type Result struct {
	// Artifacts is every produced binary + checksum, in target-matrix order.
	Artifacts []Artifact
	// UIEmbedded reports whether a web/ Vite UI was built and embedded. It is
	// false for a no-template blank server (RFC §4.1 — a UI is additive).
	UIEmbedded bool
}

// Build runs the `dockyard build` pipeline (RFC §14) for the project rooted at
// opts.ProjectDir: regenerate contracts → run the validate gate → build the
// web/ Vite UI (when present) → go build a CGo-free static binary per target
// with the UI embedded → emit a SHA-256 checksum per artifact.
//
// A validation blocker fails the build with an error wrapping
// ErrValidationBlocked; any other failure wraps ErrBuild. On success every
// Result.Artifacts entry has a binary and a checksum file on disk.
//
// Build builds fresh state per call and holds no shared mutable state.
func Build(ctx context.Context, opts Options) (Result, error) {
	logger := opts.logger()

	if opts.ProjectDir == "" {
		return Result{}, fmt.Errorf("%w: ProjectDir is required", ErrBuild)
	}
	projectDir, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return Result{}, fmt.Errorf("%w: resolve project dir: %w", ErrBuild, err)
	}
	if info, statErr := os.Stat(filepath.Join(projectDir, "dockyard.app.yaml")); statErr != nil || info.IsDir() {
		return Result{}, fmt.Errorf(
			"%w: %s holds no dockyard.app.yaml — is this a Dockyard project?", ErrBuild, projectDir)
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(projectDir, defaultOutputDir)
	}
	if outputDir, err = filepath.Abs(outputDir); err != nil {
		return Result{}, fmt.Errorf("%w: resolve output dir: %w", ErrBuild, err)
	}

	// --- stage 1: regenerate contracts -------------------------------------
	if err := regenerateContracts(ctx, projectDir, logger); err != nil {
		return Result{}, err
	}

	// --- stage 2: the validate gate (P1 at build time) ---------------------
	if opts.SkipValidate {
		logger.WarnContext(ctx, "build: validate gate skipped (test seam)")
	} else if err := runValidateGate(ctx, projectDir, logger); err != nil {
		return Result{}, err
	}

	// --- stage 3: the Vite UI build (embed ordering, RFC §14) --------------
	// The UI is built BEFORE any go build so the dist/ embed target is on disk
	// when the compiler reads the //go:embed directive.
	uiEmbedded, err := buildViteUI(ctx, projectDir, logger)
	if err != nil {
		return Result{}, err
	}

	// --- stage 4+5: go build the matrix + emit checksums -------------------
	targets := opts.Targets
	if len(targets) == 0 {
		targets = []Target{hostTarget()}
	}
	binBase := filepath.Base(projectDir)

	res := Result{UIEmbedded: uiEmbedded}
	var failures []error
	for _, t := range targets {
		logger.InfoContext(ctx, "build: compiling", slog.String("target", t.String()))
		binPath, buildErr := compileTarget(ctx, projectDir, outputDir, binBase, t)
		if buildErr != nil {
			// A per-target failure is collected, not fatal to the whole matrix
			// (D-087): one unbuildable triple must not hide a green rest of the
			// matrix. The aggregate error is returned after every target ran.
			logger.ErrorContext(ctx, "build: target failed", slog.String("target", t.String()),
				slog.String("error", buildErr.Error()))
			failures = append(failures, buildErr)
			continue
		}
		sumPath, sumErr := writeChecksum(binPath)
		if sumErr != nil {
			failures = append(failures, sumErr)
			continue
		}
		res.Artifacts = append(res.Artifacts, Artifact{
			Target: t, Path: binPath, ChecksumPath: sumPath,
		})
	}
	if len(failures) > 0 {
		return res, fmt.Errorf("%w: %d of %d target(s) failed: %w",
			ErrBuild, len(failures), len(targets), errors.Join(failures...))
	}
	logger.InfoContext(ctx, "build: complete", slog.Int("artifacts", len(res.Artifacts)),
		slog.Bool("ui_embedded", res.UIEmbedded))
	return res, nil
}

// logger returns opts.Logger or a discarding logger so Build never panics on a
// missing logger.
func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}
