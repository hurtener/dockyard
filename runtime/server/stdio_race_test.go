package server

import (
	"bytes"
	"sync"
	"testing"
)

// TestLockedWriter_SerialisesConcurrentWrites proves the depth-audit R5 / S1
// fix: the *lockedWriter shared between the SDK-output relay and the Tasks
// mount pump serialises Write calls so a large frame is never split by an
// interleaved Write from the other goroutine — the property a stdio JSON-RPC
// pipe needs (D-119).
//
// The test drives two goroutines that hammer the same writer with frames much
// larger than PIPE_BUF (4096 on macOS/Linux), then verifies every line in the
// captured output is either a complete "A" frame or a complete "B" frame —
// never an interleave. Run under -race to also catch a missed lock.
func TestLockedWriter_SerialisesConcurrentWrites(t *testing.T) {
	t.Parallel()

	// Frames much larger than the kernel's pipe atomicity bound (PIPE_BUF =
	// 4096 on macOS/Linux) and larger than io.Copy's 32 KB buffer, so a single
	// Write straddles many kernel-write chunks and an unsynchronised peer would
	// have ample opportunity to interleave.
	const frameSize = 64 * 1024
	frameA := append(bytes.Repeat([]byte{'A'}, frameSize), '\n')
	frameB := append(bytes.Repeat([]byte{'B'}, frameSize), '\n')

	var sink bytes.Buffer
	w := newLockedWriter(&sink)

	const perWriter = 32
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < perWriter; i++ {
			if _, err := w.Write(frameA); err != nil {
				t.Errorf("Write A: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < perWriter; i++ {
			if _, err := w.Write(frameB); err != nil {
				t.Errorf("Write B: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	// Each line must be a pure A or pure B run of frameSize bytes — any
	// interleave shows up as a mixed-character line. Splitting on '\n' is safe:
	// every frame ends with one and a serialised writer cannot lose any.
	lines := bytes.Split(sink.Bytes(), []byte{'\n'})
	wantLines := perWriter*2 + 1 // trailing "" after the last '\n'
	if len(lines) != wantLines {
		t.Fatalf("got %d split parts, want %d (perWriter*2 + trailing)", len(lines), wantLines)
	}
	if last := lines[len(lines)-1]; len(last) != 0 {
		t.Fatalf("trailing fragment after last newline: %d bytes", len(last))
	}
	var countA, countB int
	for i, line := range lines[:len(lines)-1] {
		if len(line) != frameSize {
			t.Fatalf("line %d: length = %d, want %d (interleaved write)", i, len(line), frameSize)
		}
		switch line[0] {
		case 'A':
			if bytes.IndexByte(line, 'B') != -1 {
				t.Fatalf("line %d: mixed A/B (interleave): %q…", i, line[:32])
			}
			countA++
		case 'B':
			if bytes.IndexByte(line, 'A') != -1 {
				t.Fatalf("line %d: mixed B/A (interleave): %q…", i, line[:32])
			}
			countB++
		default:
			t.Fatalf("line %d: unexpected first byte %q", i, line[0])
		}
	}
	if countA != perWriter || countB != perWriter {
		t.Fatalf("counts: A=%d B=%d, want %d each", countA, countB, perWriter)
	}
}
