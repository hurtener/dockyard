// Command changelogx extracts one version's section from a
// Keep-a-Changelog CHANGELOG.md and writes it to stdout (or a file).
// Consumed by .github/workflows/release.yml as the GitHub-Release-body
// source on a tag push.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/hurtener/dockyard/internal/changelogx"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		// ErrSectionNotFound is a documented exit-2 condition so the
		// release workflow can branch ("no section for this tag, fail
		// the release"). Other errors are exit-1.
		if errors.Is(err, changelogx.ErrSectionNotFound) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr *os.File) error {
	fs := flag.NewFlagSet("changelogx", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		file    = fs.String("file", "CHANGELOG.md", "path to the CHANGELOG.md to read")
		version = fs.String("version", "", "version section to extract (e.g. 1.0.0 or v1.0.0)")
		outPath = fs.String("out", "", "optional output file; empty = stdout")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		fs.Usage()
		return fmt.Errorf("-version is required")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("read %s: %w", *file, err)
	}
	body, err := changelogx.ExtractSection(b, *version)
	if err != nil {
		return fmt.Errorf("extract %q from %s: %w", *version, *file, err)
	}
	if *outPath == "" {
		if _, err := stdout.Write(body); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	// 0o644 mirrors `internal/buildpkg.writeChecksum` — a release
	// artifact / metadata file is publishable, not a secret.
	if err := os.WriteFile(*outPath, body, 0o644); err != nil { //nolint:gosec
		return fmt.Errorf("write %s: %w", *outPath, err)
	}
	return nil
}
