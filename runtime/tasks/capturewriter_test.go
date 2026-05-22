package tasks

import (
	"net/http"
	"testing"
)

// This file holds a Phase 21.5 edge-case unit test for the captureWriter — the
// buffering http.ResponseWriter the Tasks HTTP mount uses to capture and
// rewrite an initialize response. Its WriteHeader branch was uncovered; this
// test exercises the full ResponseWriter surface directly.

func TestCaptureWriter(t *testing.T) {
	t.Parallel()

	cw := newCaptureWriter()

	// A fresh captureWriter defaults to 200 OK with an empty header set.
	if cw.status != http.StatusOK {
		t.Fatalf("fresh captureWriter status = %d, want %d", cw.status, http.StatusOK)
	}
	if cw.Header() == nil {
		t.Fatal("Header() returned nil")
	}

	// Header() exposes a mutable http.Header the caller can populate.
	cw.Header().Set("Content-Type", "application/json")
	if got := cw.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Header Content-Type = %q, want application/json", got)
	}

	// WriteHeader records the status code.
	cw.WriteHeader(http.StatusBadGateway)
	if cw.status != http.StatusBadGateway {
		t.Fatalf("after WriteHeader, status = %d, want %d", cw.status, http.StatusBadGateway)
	}

	// Write buffers the body and reports the byte count.
	payload := []byte(`{"ok":true}`)
	n, err := cw.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write returned n=%d, want %d", n, len(payload))
	}
	if got := cw.body.String(); got != string(payload) {
		t.Fatalf("buffered body = %q, want %q", got, payload)
	}

	// A second Write appends rather than replacing.
	if _, err := cw.Write([]byte("tail")); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if got := cw.body.String(); got != `{"ok":true}tail` {
		t.Fatalf("body after append = %q", got)
	}
}
