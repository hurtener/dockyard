package tasks

import (
	"encoding/base64"
	"strconv"
)

// The tasks/list cursor is an opaque pagination token (vendored spec, "Task
// Listing" rule 3 — requestors MUST treat it as opaque). The in-memory driver
// encodes a 1-past-the-end offset; base64 keeps it visibly opaque so a
// requestor is not tempted to parse it. A future durable driver may encode a
// different token behind the same encode/decode pair.

const cursorPrefix = "dyt1:" // versioned, so a token shape change is detectable

// encodeCursor returns the opaque cursor token for offset i.
func encodeCursor(i int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.Itoa(i)))
}

// decodeCursor parses a cursor token produced by encodeCursor back to its
// offset. It returns an error for any token it did not produce.
func decodeCursor(tok string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return 0, err
	}
	s := string(raw)
	if len(s) <= len(cursorPrefix) || s[:len(cursorPrefix)] != cursorPrefix {
		return 0, strconv.ErrSyntax
	}
	return strconv.Atoi(s[len(cursorPrefix):])
}
