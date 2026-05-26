package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// claudeHostID is the id the Claude host profile registers under.
const claudeHostID = "claude"

// claudeContentApex is the apex domain Claude serves dedicated App origins from.
// Claude requires every App to render from a signed subdomain of this apex —
// brief 01 §2.5, §2.8.
const claudeContentApex = "claudemcpcontent.com"

// claudeHashHexLen is the length of the hex hash label Claude prefixes onto the
// content apex. Brief 01 §2.5 documents the form as `<hash32>` — 32 hex
// characters, i.e. the first 16 bytes (128 bits) of the SHA-256 digest. 128
// bits is collision-resistant for the origin-allowlisting use and keeps the
// label well inside the 63-character DNS-label limit (D-063).
const claudeHashHexLen = 32

// claudeHostProfile derives Claude's signed, dedicated content origin for an
// App's iframe (`_meta.ui.domain`). Claude does not serve an App from an
// arbitrary developer-chosen origin: it requires a stable, signed subdomain of
// claudemcpcontent.com derived from the MCP server URL, so a malicious server
// cannot claim another server's origin (brief 01 §2.5, §4 sharp edge 3).
//
// This is the canonical example of a host-specific *derivation function* living
// behind the host-profile seam — an algorithm, never a capability matrix
// (RFC §7.5, D-011, D-012). The Apps core never names Claude; only this driver
// file does.
type claudeHostProfile struct{}

// ID implements HostProfile.
func (claudeHostProfile) ID() string { return claudeHostID }

// DeriveDomain implements HostProfile: it derives the signed
// `<hash>.claudemcpcontent.com` content origin.
//
// The hash input binds both the MCP server URL — so each server gets a distinct
// origin it cannot forge — and the App's domain label — so two Apps on one
// server can request two distinct dedicated origins. The digest is SHA-256; the
// label is the lowercase-hex encoding of its first 16 bytes (D-063).
//
// An empty label means the App declared no dedicated origin: DeriveDomain
// returns "" with a nil error so `_meta.ui.domain` is omitted. A non-empty
// label with an empty serverURL is a misuse the runtime cannot derive a stable
// signed origin for, so it returns a typed error rather than a forgeable
// origin.
func (claudeHostProfile) DeriveDomain(label, serverURL string) (string, error) {
	if label == "" {
		return "", nil
	}
	if serverURL == "" {
		return "", fmt.Errorf(
			"%w: claude host profile cannot derive a signed origin without a server URL",
			ErrInvalidApp)
	}
	// Bind server URL and label with a separator that cannot appear in either
	// half ambiguously, so distinct (url, label) pairs cannot collide by
	// concatenation.
	digest := sha256.Sum256([]byte(serverURL + "\x00" + label))
	hash := hex.EncodeToString(digest[:])[:claudeHashHexLen]
	return hash + "." + claudeContentApex, nil
}

// RequiresServerURL implements HostProfile: Claude's profile binds the
// signed-origin derivation to the MCP server URL (brief 01 §2.5,
// D-063/D-064) so distinct servers cannot forge each other's origin. A
// non-empty domain label demands a non-empty serverURL; the capability-
// degradation testgate category honours that by exempting Claude from
// the empty-URL derivation rather than synthesising a placeholder URL
// to dodge the invariant (D-165 — supersedes D-145's workaround).
func (claudeHostProfile) RequiresServerURL() bool { return true }

// init registers the Claude host profile via the seam (AGENTS.md §4.4).
func init() {
	if err := RegisterHostProfile(claudeHostProfile{}); err != nil {
		// See hostprofile_generic.go init() — a built-in driver's registration
		// can only fail on an impossible duplicate; never panic from init().
		_ = err
	}
}
