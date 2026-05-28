package main

import (
	"os"
	"strings"
	"testing"
)

// devNull returns an *os.File for /dev/null, closed by the test cleanup. The
// run() signature takes *os.File; the error-path cases below never write, so
// the sink is only there to satisfy the type.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestRun_ArgValidation covers the two mode-selection / arg-validation
// branches that gate the toolchain-touching work — the wiring a unit test can
// exercise without a git repo or a CHANGELOG on disk.
func TestRun_ArgValidation(t *testing.T) {
	t.Parallel()
	null := devNull(t)
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"supplement without -from": {
			args:    []string{"-supplement", "-to", "HEAD"},
			wantErr: "-from is required",
		},
		"extract without -version": {
			args:    []string{},
			wantErr: "-version is required",
		},
		"extract missing file": {
			args:    []string{"-version", "1.0.0", "-file", "does-not-exist.md"},
			wantErr: "read does-not-exist.md",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := run(tc.args, null, null)
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
