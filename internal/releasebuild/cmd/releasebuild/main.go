// Command releasebuild drives Dockyard's V1 release pipeline — the step
// the `.github/workflows/release.yml` workflow runs on a `v*` tag push.
//
// Usage:
//
//	releasebuild -version v1.0.0 -output ./dist/release
//	releasebuild -version v1.0.0 -output ./dist/release -host-only
//	releasebuild -version v1.0.0 -output ./dist/release -project /path/to/dockyard
//
// The default matrix is the RFC §14 cross-compile set
// (darwin/linux/windows × amd64/arm64). -host-only narrows to the
// runner's GOOS/GOARCH and is the shape `workflow_dispatch` dry-runs
// use to verify the pipeline without a 30-minute matrix build.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/hurtener/dockyard/internal/releasebuild"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("releasebuild", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		version   = fs.String("version", "", "release version (e.g. v1.0.0)")
		outputDir = fs.String("output", "", "directory release artifacts are written into")
		project   = fs.String("project", ".", "Dockyard repo root (directory holding go.mod)")
		cmdPath   = fs.String("cmd-path", "./cmd/dockyard", "Go import path of the dockyard CLI main package, relative to -project")
		base      = fs.String("base", "dockyard", "stem of the published artifact filename")
		hostOnly  = fs.Bool("host-only", false, "build only the host GOOS/GOARCH (for dry-runs)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		fs.Usage()
		return fmt.Errorf("-version is required")
	}
	if *outputDir == "" {
		fs.Usage()
		return fmt.Errorf("-output is required")
	}

	// A text logger so the workflow log surfaces release progress
	// without JSON parsing. Mirrors `dockyard dev`'s posture (CLAUDE.md
	// §5: JSON in production; text under dev — a release CI step is
	// "dev" in the sense it is a developer-facing log).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	opts := releasebuild.Options{
		Version:    *version,
		ProjectDir: *project,
		OutputDir:  *outputDir,
		CmdPath:    *cmdPath,
		BinaryBase: *base,
		Logger:     logger,
	}
	if *hostOnly {
		opts.Targets = []releasebuild.Target{releasebuild.HostTarget()}
	}

	res, err := releasebuild.Release(context.Background(), opts)
	if err != nil {
		return err
	}
	// The stdout summary is best-effort presentation; a write error on
	// os.Stdout means the parent process closed it, in which case the
	// release has already happened on disk and there is nothing useful
	// to do with the error.
	_, _ = fmt.Fprintln(os.Stdout, "RELEASE OK")
	_, _ = fmt.Fprintf(os.Stdout, "  version:   %s\n", res.Version)
	_, _ = fmt.Fprintf(os.Stdout, "  output:    %s\n", res.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "  artifacts: %d\n", len(res.Artifacts))
	_, _ = fmt.Fprintf(os.Stdout, "  checksums: %s\n", res.ChecksumsFile)
	for _, a := range res.Artifacts {
		_, _ = fmt.Fprintf(os.Stdout, "  - %s  %s\n", a.SHA256, a.Path)
	}
	return nil
}
