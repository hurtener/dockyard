package buildpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// checksumExt is the suffix appended to an artifact path to name its checksum
// sidecar file — the convention release tooling (Phase 30) expects.
const checksumExt = ".sha256"

// writeChecksum computes the SHA-256 of the artifact at binPath and writes a
// sidecar checksum file next to it. The file format is the standard
// `sha256sum` line — "<hex>  <basename>\n" — so a developer can verify an
// artifact with the stock `sha256sum -c` tool. It returns the checksum file's
// path.
func writeChecksum(binPath string) (string, error) {
	sum, err := sha256File(binPath)
	if err != nil {
		return "", err
	}
	sumPath := binPath + checksumExt
	line := fmt.Sprintf("%s  %s\n", sum, filepath.Base(binPath))
	if err := os.WriteFile(sumPath, []byte(line), 0o644); err != nil { //nolint:gosec // a published checksum is not a secret
		return "", fmt.Errorf("%w: write checksum %s: %w", ErrBuild, sumPath, err)
	}
	return sumPath, nil
}

// sha256File returns the lowercase hex SHA-256 digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is a build artifact in a caller-supplied output dir
	if err != nil {
		return "", fmt.Errorf("%w: open artifact %s: %w", ErrBuild, path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("%w: hash artifact %s: %w", ErrBuild, path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
