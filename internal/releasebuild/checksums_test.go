package releasebuild

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteChecksumSidecar exercises the per-artifact sidecar shape
// against a small synthetic file.
func TestWriteChecksumSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "dockyard-v1.0.0-linux-amd64")
	body := []byte("not a real binary; just bytes for hashing\n")
	if err := os.WriteFile(bin, body, 0o755); err != nil { //nolint:gosec // a test fixture binary
		t.Fatalf("write fixture: %v", err)
	}
	sumPath, sum, err := writeChecksumSidecar(bin)
	if err != nil {
		t.Fatalf("writeChecksumSidecar: %v", err)
	}
	if sumPath != bin+".sha256" {
		t.Errorf("sidecar path = %q, want %q", sumPath, bin+".sha256")
	}
	h := sha256.Sum256(body)
	if sum != hex.EncodeToString(h[:]) {
		t.Errorf("digest mismatch: got %q want %q", sum, hex.EncodeToString(h[:]))
	}
	got, err := os.ReadFile(sumPath) //nolint:gosec // sidecar path is per-test under t.TempDir()
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	want := sum + "  " + filepath.Base(bin) + "\n"
	if string(got) != want {
		t.Errorf("sidecar body = %q, want %q", string(got), want)
	}
}

// TestWriteChecksumsFile sorts artifacts by basename and produces a
// deterministic aggregate.
func TestWriteChecksumsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "checksums.txt")
	// Deliberately out-of-order input so we exercise the sort.
	artifacts := []Artifact{
		{Path: filepath.Join(dir, "z.bin"), SHA256: strings.Repeat("a", 64)},
		{Path: filepath.Join(dir, "a.bin"), SHA256: strings.Repeat("b", 64)},
		{Path: filepath.Join(dir, "m.bin"), SHA256: strings.Repeat("c", 64)},
	}
	if err := writeChecksumsFile(out, artifacts); err != nil {
		t.Fatalf("writeChecksumsFile: %v", err)
	}
	body, err := os.ReadFile(out) //nolint:gosec // aggregate path is per-test under t.TempDir()
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	want := strings.Join([]string{
		strings.Repeat("b", 64) + "  a.bin",
		strings.Repeat("c", 64) + "  m.bin",
		strings.Repeat("a", 64) + "  z.bin",
		"",
	}, "\n")
	if string(body) != want {
		t.Errorf("aggregate body =\n%q\nwant:\n%q", string(body), want)
	}
}

// TestWriteChecksumsFile_Empty rejects a zero-artifact input — a release
// with nothing to publish is a config bug.
func TestWriteChecksumsFile_Empty(t *testing.T) {
	t.Parallel()
	err := writeChecksumsFile(filepath.Join(t.TempDir(), "checksums.txt"), nil)
	if !errors.Is(err, ErrRelease) {
		t.Errorf("expected ErrRelease for an empty artifact list; got %v", err)
	}
}
