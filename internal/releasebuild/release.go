package releasebuild

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hurtener/dockyard/internal/buildpkg"
)

// ErrRelease is the sentinel wrapping a release driver failure. A failure
// inside `go build` propagates wrapped in ErrRelease; an IO fault on
// publish does the same. Callers branch with errors.Is(err, ErrRelease).
var ErrRelease = errors.New("dockyard/internal/releasebuild: release failed")

// defaultProjectDir is the Dockyard repository root the release pipeline
// is run against — the directory holding go.mod. Empty Options.ProjectDir
// defaults to the current working directory, which is what the GitHub
// Actions runner gives us after `actions/checkout`.
const defaultProjectDir = "."

// defaultCmdPath is the Go-import path of the dockyard CLI main package,
// relative to the Dockyard module root. The release driver `go build`s this
// once per cross-compile target.
const defaultCmdPath = "./cmd/dockyard"

// defaultBinaryBase is the published binary's stem — the user-facing name
// inside every "dockyard-<version>-<os>-<arch>" artifact filename.
const defaultBinaryBase = "dockyard"

// checksumsFilename is the aggregate `sha256sum -c`-compatible file the
// release driver writes alongside the per-artifact .sha256 sidecars. A
// downloader can verify every release artifact in one command:
//
//	sha256sum -c checksums.txt
const checksumsFilename = "checksums.txt"

// goBuildLDFlags mirrors the symbol/DWARF strip flags `make build` uses for
// the dockyard CLI itself (and that internal/buildpkg.compileTarget uses
// for project builds), so the release artifacts and the developer-built
// `bin/dockyard` are produced with the same toolchain posture.
const goBuildLDFlags = "-s -w"

// versionRE accepts a semver-shaped version with or without the `v` prefix.
// The release pipeline tag is shaped `v1.0.0` per docs/RELEASING.md; the
// driver also accepts `1.0.0` for callers that strip the prefix. The
// intentionally permissive pattern admits stable releases and the standard
// pre-release / build metadata forms (`v1.0.0-rc.1`, `v1.0.0+build.1`); it
// rejects strings that contain a path separator or whitespace, which is the
// only character class that would break the artifact-filename derivation.
var versionRE = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.\-+]*$`)

// Options configures one Release invocation.
type Options struct {
	// Version is the release version (e.g. "v1.0.0" or "1.0.0"). Required.
	// The driver canonicalises the form once: every produced artifact
	// embeds the "v"-prefixed shape so the published filename matches
	// the GitHub Release tag.
	Version string
	// ProjectDir is the Dockyard repository root — the directory holding
	// go.mod. Empty means the current working directory.
	ProjectDir string
	// OutputDir is the directory release artifacts land in. Required —
	// the release workflow points at a fresh tempdir under runner.temp
	// so a re-run does not see a previous run's stale files.
	OutputDir string
	// CmdPath overrides the Go-import path of the dockyard CLI main
	// package. Defaults to "./cmd/dockyard". Tests use this to point
	// at a fixture main package.
	CmdPath string
	// Targets overrides the cross-compile matrix; empty means the RFC §14
	// DefaultMatrix (darwin/linux/windows × amd64/arm64). Tests use this
	// to narrow the matrix to the host-only target so a release dry-run
	// stays fast and CGo-free on any GitHub runner.
	Targets []buildpkg.Target
	// BinaryBase overrides the published artifact stem. Defaults to
	// "dockyard". Tests pass a unique name to keep parallel runs
	// untangled.
	BinaryBase string
	// Logger receives structured progress output. A nil Logger falls
	// back to a discarding handler so Release never panics on a missing
	// logger.
	Logger *slog.Logger
}

// Artifact describes one published binary + its checksum sidecar.
type Artifact struct {
	// Target is the GOOS/GOARCH this artifact was built for.
	Target buildpkg.Target
	// Path is the absolute path of the published binary.
	Path string
	// ChecksumPath is the absolute path of the binary's .sha256 sidecar.
	ChecksumPath string
	// SHA256 is the binary's lowercase hex SHA-256 digest — convenient
	// for the aggregate checksums writer and any direct caller.
	SHA256 string
}

// Result describes what Release produced.
type Result struct {
	// Version is the canonicalised release version (always "v"-prefixed).
	Version string
	// OutputDir is the absolute path of the directory artifacts landed in.
	OutputDir string
	// Artifacts is every published binary + checksum, in matrix order.
	Artifacts []Artifact
	// ChecksumsFile is the absolute path of the aggregate checksums.txt.
	ChecksumsFile string
}

// Release runs the V1 release pipeline against the dockyard CLI source at
// opts.ProjectDir and publishes the artifacts under opts.OutputDir.
//
// For each target in opts.Targets (or the RFC §14 DefaultMatrix when the
// slice is empty), Release runs `go build ./cmd/dockyard` with
// CGO_ENABLED=0 + GOOS/GOARCH set, names the resulting binary
// "dockyard-<version>-<os>-<arch>[.exe]", writes the per-artifact .sha256
// sidecar, and finally writes the aggregate `checksums.txt`.
//
// A failure wraps `ErrRelease` (with the stderr buffered into the error
// message so a CI log surfaces it without artifact upload).
//
// Release does not run `make preflight` — that gate runs as a separate
// step in `.github/workflows/release.yml` *before* the release driver is
// invoked. Keeping the two steps separate makes it possible to dry-run
// the release packaging without re-running the full preflight, and lets
// `act` or a local run inspect the release artifacts on their own.
//
// Release holds no shared mutable state; concurrent invocations with
// distinct Options are safe.
func Release(ctx context.Context, opts Options) (Result, error) {
	if err := validateOpts(&opts); err != nil {
		return Result{}, err
	}
	logger := opts.logger()
	version := canonicalVersion(opts.Version)

	projectDir, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return Result{}, fmt.Errorf("%w: resolve project dir: %w", ErrRelease, err)
	}
	if info, statErr := os.Stat(filepath.Join(projectDir, "go.mod")); statErr != nil || info.IsDir() {
		return Result{}, fmt.Errorf(
			"%w: %s holds no go.mod — is this the Dockyard repo root?",
			ErrRelease, projectDir)
	}
	outputDir, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return Result{}, fmt.Errorf("%w: resolve output dir: %w", ErrRelease, err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil { //nolint:gosec // a release output dir is browsable, not a secret
		return Result{}, fmt.Errorf("%w: create output dir %s: %w", ErrRelease, outputDir, err)
	}

	targets := opts.Targets
	if len(targets) == 0 {
		targets = buildpkg.DefaultMatrix()
	}
	binaryBase := opts.BinaryBase
	if binaryBase == "" {
		binaryBase = defaultBinaryBase
	}
	cmdPath := opts.CmdPath
	if cmdPath == "" {
		cmdPath = defaultCmdPath
	}

	logger.InfoContext(ctx, "release: starting",
		slog.String("version", version),
		slog.String("project_dir", projectDir),
		slog.String("output_dir", outputDir),
		slog.String("cmd_path", cmdPath),
		slog.Int("targets", len(targets)),
	)

	out := Result{
		Version:   version,
		OutputDir: outputDir,
	}
	var failures []error
	for _, t := range targets {
		artifactName := publishedArtifactName(binaryBase, version, t)
		artifactPath := filepath.Join(outputDir, artifactName)

		logger.InfoContext(ctx, "release: compiling",
			slog.String("target", t.String()),
			slog.String("artifact", artifactName))

		if err := compileTarget(ctx, projectDir, cmdPath, artifactPath, t); err != nil {
			// A per-target failure is collected, not fatal to the
			// whole matrix (mirrors internal/buildpkg.Build's
			// D-087 behaviour). The aggregate error is returned
			// after every target ran.
			logger.ErrorContext(ctx, "release: target failed",
				slog.String("target", t.String()),
				slog.String("error", err.Error()))
			failures = append(failures, err)
			continue
		}
		sumPath, sum, err := writeChecksumSidecar(artifactPath)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		out.Artifacts = append(out.Artifacts, Artifact{
			Target:       t,
			Path:         artifactPath,
			ChecksumPath: sumPath,
			SHA256:       sum,
		})
		logger.InfoContext(ctx, "release: published",
			slog.String("target", t.String()),
			slog.String("path", artifactPath),
			slog.String("sha256", sum),
		)
	}
	if len(failures) > 0 {
		return out, fmt.Errorf("%w: %d of %d target(s) failed: %w",
			ErrRelease, len(failures), len(targets), errors.Join(failures...))
	}

	checksumsFile := filepath.Join(outputDir, checksumsFilename)
	if err := writeChecksumsFile(checksumsFile, out.Artifacts); err != nil {
		return out, err
	}
	out.ChecksumsFile = checksumsFile

	logger.InfoContext(ctx, "release: complete",
		slog.Int("artifacts", len(out.Artifacts)),
		slog.String("checksums", checksumsFile),
	)
	return out, nil
}

// compileTarget runs `go build` for one cross-compile target, writing the
// produced artifact to outputPath. CGO_ENABLED=0 is pinned so the artifact
// is CGo-free and statically linked (RFC §14, CLAUDE.md §5/§6). GOOS/GOARCH
// select the target.
//
// The build runs in projectDir so the Go module the dockyard CLI lives in
// is the build's working directory; the build target is `cmdPath` (an
// import path like "./cmd/dockyard" — relative to projectDir).
//
// Build output (stdout + stderr) is captured into the returned error on
// failure so a CI step's log surfaces the underlying go build diagnostics
// without artifact upload.
func compileTarget(ctx context.Context, projectDir, cmdPath, outputPath string, t Target) error {
	cmd := exec.CommandContext(ctx, "go", "build", //nolint:gosec // a fixed `go build` of the dockyard CLI source
		"-ldflags="+goBuildLDFlags,
		"-o", outputPath,
		cmdPath,
	)
	cmd.Dir = projectDir
	// CGO_ENABLED=0 is the non-negotiable shipped-artifact guarantee
	// (CLAUDE.md §5/§6). GOOS/GOARCH cross-compile to the target triple.
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+t.OS,
		"GOARCH="+t.Arch,
	)
	var captured bytes.Buffer
	cmd.Stdout = &captured
	cmd.Stderr = &captured
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: go build %s: %w\n%s", ErrRelease, t, err, captured.String())
	}
	return nil
}

// Target re-exports buildpkg.Target so callers do not have to import both
// packages — the release driver and its tests almost always work with the
// build matrix as one unit.
type Target = buildpkg.Target

// DefaultMatrix re-exports buildpkg.DefaultMatrix for the same reason.
func DefaultMatrix() []Target { return buildpkg.DefaultMatrix() }

// HostTarget returns the GOOS/GOARCH of the runner the driver is on. The
// release dry-run uses this to narrow the matrix to one fast target.
func HostTarget() Target { return Target{OS: runtimeGOOS(), Arch: runtimeGOARCH()} }

// Indirection through tiny wrappers so the runtime package import remains
// implicit — keeps the package's import set as narrow as the build needs.
func runtimeGOOS() string   { return goEnv("GOOS") }
func runtimeGOARCH() string { return goEnv("GOARCH") }

func goEnv(key string) string {
	// Prefer the process env (the release runner sets these explicitly
	// in tests), then fall back to `go env`. We avoid importing
	// "runtime" so the package's import set stays narrow.
	if v := os.Getenv(key); v != "" {
		return v
	}
	out, err := exec.Command("go", "env", key).Output() //nolint:gosec // fixed `go env` call
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// validateOpts checks the required Options fields and sets light defaults.
func validateOpts(opts *Options) error {
	if opts.Version == "" {
		return fmt.Errorf("%w: Version is required", ErrRelease)
	}
	if !versionRE.MatchString(opts.Version) {
		return fmt.Errorf("%w: Version %q is not a semver-shaped string", ErrRelease, opts.Version)
	}
	if opts.OutputDir == "" {
		return fmt.Errorf("%w: OutputDir is required", ErrRelease)
	}
	if opts.ProjectDir == "" {
		opts.ProjectDir = defaultProjectDir
	}
	return nil
}

// logger returns opts.Logger or a discarding logger so Release never panics.
func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// canonicalVersion returns the "v"-prefixed form of a semver-shaped version.
func canonicalVersion(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// publishedArtifactName returns the user-facing artifact filename:
//
//	"<base>-<version>-<os>-<arch>[.exe]"
//
// e.g. "dockyard-v1.0.0-linux-amd64", "dockyard-v1.0.0-windows-arm64.exe".
func publishedArtifactName(base, version string, t Target) string {
	exe := ""
	if t.OS == "windows" {
		exe = ".exe"
	}
	return fmt.Sprintf("%s-%s-%s-%s%s", base, version, t.OS, t.Arch, exe)
}
