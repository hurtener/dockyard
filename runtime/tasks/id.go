package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// IDFunc generates a fresh, unique task identifier. It is the seam through
// which Phase 14 can swap the generator (e.g. to bind an auth-context prefix);
// Phase 13's default is [CryptoID].
type IDFunc func() (string, error)

// idBytes is the entropy of a generated task ID: 16 bytes = 128 bits.
//
// Task IDs are the only access-control mechanism absent an auth context
// (vendored spec, "Task Isolation and Access Control"; brief 02 §4.5), so the
// default generator MUST be crypto-strong with enough entropy to defeat
// guessing. 128 bits is the spec-recommended floor; Phase 13 starts here so
// Phase 14 never has to rip out a weak scheme.
const idBytes = 16

// CryptoID is the default [IDFunc]: a 128-bit cryptographically random
// identifier, hex-encoded. It draws from crypto/rand — never math/rand.
func CryptoID() (string, error) {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("dockyard/runtime/tasks: generate task id: %w", err)
	}
	return "task_" + hex.EncodeToString(b[:]), nil
}
