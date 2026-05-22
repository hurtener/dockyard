package generate

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes content to dir/name (creating parent directories) and fails
// the test on error. It is the shared fixture-builder for the generate tests.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
