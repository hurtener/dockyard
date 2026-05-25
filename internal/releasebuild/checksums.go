package releasebuild

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// checksumExt is the suffix appended to an artifact path to name its
// checksum sidecar file. Mirrors `internal/buildpkg`'s convention so a
// release artifact and a `dockyard build` artifact verify the same way.
const checksumExt = ".sha256"

// writeChecksumSidecar computes the SHA-256 of the binary at path,
// writes a `sha256sum`-compatible sidecar next to it, and returns the
// sidecar's path + the digest. The digest is also returned so the
// aggregate checksums writer does not have to re-hash the same file.
func writeChecksumSidecar(binPath string) (string, string, error) {
	sum, err := sha256File(binPath)
	if err != nil {
		return "", "", err
	}
	sumPath := binPath + checksumExt
	line := fmt.Sprintf("%s  %s\n", sum, filepath.Base(binPath))
	if err := os.WriteFile(sumPath, []byte(line), 0o644); err != nil { //nolint:gosec // a checksum is publishable, not a secret
		return "", "", fmt.Errorf("%w: write checksum %s: %w", ErrRelease, sumPath, err)
	}
	return sumPath, sum, nil
}

// writeChecksumsFile writes the aggregate `sha256sum -c`-compatible file
// covering every published artifact. The file is sorted by published
// filename so a re-run on the same matrix produces a byte-identical
// aggregate (deterministic release artifacts make build-comparison
// audits practical).
func writeChecksumsFile(path string, artifacts []Artifact) error {
	if len(artifacts) == 0 {
		return fmt.Errorf("%w: no artifacts to checksum", ErrRelease)
	}
	// Sort by basename so the output is stable regardless of the
	// matrix iteration order.
	sorted := make([]Artifact, len(artifacts))
	copy(sorted, artifacts)
	sort.Slice(sorted, func(i, j int) bool {
		return filepath.Base(sorted[i].Path) < filepath.Base(sorted[j].Path)
	})
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644) //nolint:gosec // checksums file is publishable, not a secret
	if err != nil {
		return fmt.Errorf("%w: open %s: %w", ErrRelease, path, err)
	}
	defer func() { _ = f.Close() }()
	for _, a := range sorted {
		if _, err := fmt.Fprintf(f, "%s  %s\n", a.SHA256, filepath.Base(a.Path)); err != nil {
			return fmt.Errorf("%w: write %s: %w", ErrRelease, path, err)
		}
	}
	return f.Sync()
}

// sha256File returns the lowercase hex SHA-256 digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is a build artifact in a controlled output dir
	if err != nil {
		return "", fmt.Errorf("%w: open %s: %w", ErrRelease, path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("%w: hash %s: %w", ErrRelease, path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
