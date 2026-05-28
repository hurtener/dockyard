// Command changelogx serves the release pipeline's CHANGELOG needs:
//
//   - the default mode extracts one version's section from a
//     Keep-a-Changelog CHANGELOG.md and writes it to stdout (or a file) —
//     the GitHub-Release-body source on a tag push;
//   - the -supplement mode renders the Conventional-Commits supplement
//     (D-167) for a commit range and appends it below the extracted body —
//     the auto-generated "what landed in detail" companion to the
//     hand-authored narrative.
//
// Consumed by .github/workflows/release.yml.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		file       = fs.String("file", "CHANGELOG.md", "path to the CHANGELOG.md to read")
		version    = fs.String("version", "", "version section to extract (e.g. 1.0.0 or v1.0.0)")
		outPath    = fs.String("out", "", "optional output file; empty = stdout")
		appendOut  = fs.Bool("append", false, "append to -out instead of truncating it (used with -supplement)")
		supplement = fs.Bool("supplement", false, "render the Conventional-Commits supplement for -from..-to instead of extracting a section")
		from       = fs.String("from", "", "supplement mode: the start ref of the commit range (e.g. the previous tag)")
		to         = fs.String("to", "HEAD", "supplement mode: the end ref of the commit range")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *supplement {
		return runSupplement(*from, *to, *outPath, *appendOut, stdout)
	}
	return runExtract(*file, *version, *outPath, stdout)
}

// runExtract is the default mode: extract one version's section body.
func runExtract(file, version, outPath string, stdout *os.File) error {
	if version == "" {
		return fmt.Errorf("-version is required")
	}
	b, err := os.ReadFile(file) //nolint:gosec // file is a CLI flag naming the CHANGELOG to read
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	body, err := changelogx.ExtractSection(b, version)
	if err != nil {
		return fmt.Errorf("extract %q from %s: %w", version, file, err)
	}
	return writeOut(outPath, body, false, stdout)
}

// runSupplement renders the Conventional-Commits supplement for the
// from..to commit range. An empty -from is rejected (a supplement against
// every ancestor of -to is never what the release wants). When no commit
// survives the classifier the block is empty and nothing is written —
// safe to append unconditionally from the workflow.
func runSupplement(from, to, outPath string, appendOut bool, stdout *os.File) error {
	if from == "" {
		return fmt.Errorf("-from is required with -supplement")
	}
	subjects, err := gitLogSubjects(from + ".." + to)
	if err != nil {
		return err
	}
	commits := make([]changelogx.Commit, 0, len(subjects))
	for _, s := range subjects {
		commits = append(commits, changelogx.ParseCommit(s))
	}
	block := changelogx.Supplement(commits)
	if block == "" {
		return nil // nothing to append
	}
	// When appending below an existing release body, separate with a blank
	// line; the block itself opens with the "---" rule.
	if appendOut && outPath != "" {
		block = "\n" + block
	}
	return writeOut(outPath, []byte(block), appendOut, stdout)
}

// gitLogSubjects returns the subject line of every non-merge commit in the
// given range, newest first. A range whose endpoints do not resolve is a
// hard error (the workflow computed them from real tags).
func gitLogSubjects(rng string) ([]string, error) {
	cmd := exec.Command("git", "log", "--no-merges", "--pretty=format:%s", rng) //nolint:gosec // rng is composed from workflow-supplied tag refs
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log %s: %w\n%s", rng, err, strings.TrimSpace(stderr.String()))
	}
	var lines []string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// writeOut writes content to stdout (empty path), or to a file — truncating
// or appending per appendOut. 0o644 mirrors internal/buildpkg.writeChecksum:
// a release artifact / metadata file is publishable, not a secret.
func writeOut(outPath string, content []byte, appendOut bool, stdout *os.File) error {
	if outPath == "" {
		if _, err := stdout.Write(content); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendOut {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(outPath, flags, 0o644) //nolint:gosec // release metadata file, not a secret
	if err != nil {
		return fmt.Errorf("open %s: %w", outPath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}
