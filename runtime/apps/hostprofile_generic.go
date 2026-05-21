package apps

// genericHostID is the id of the default, host-agnostic profile.
const genericHostID = "generic"

// genericHostProfile is the default host profile: it performs no host-specific
// derivation and passes the developer's declared domain label through verbatim.
//
// This is exactly the behaviour Phase 09 plumbed before host profiles existed —
// `App.Domain` carried straight onto `_meta.ui.domain` (D-049). It is the
// correct, spec-faithful default for any host that has not negotiated a
// signed-origin scheme: `_meta.ui.domain` is just a stable origin *request*,
// and a host that does not specialise it reads the label as-is (brief 01 §2.5).
type genericHostProfile struct{}

// ID implements HostProfile.
func (genericHostProfile) ID() string { return genericHostID }

// DeriveDomain implements HostProfile: it returns the label unchanged. An empty
// label yields an empty origin so the runtime omits `_meta.ui.domain`.
func (genericHostProfile) DeriveDomain(label, _ string) (string, error) {
	return label, nil
}

// init registers the generic profile. It is the always-present default the
// registry falls back to (HostProfileFor("") and DefaultHostProfile()).
func init() {
	if err := RegisterHostProfile(genericHostProfile{}); err != nil {
		// A duplicate registration of a built-in driver is a programming
		// error in this package, not a runtime condition — but per AGENTS.md
		// §5 we never panic for control flow. The only way this fails is two
		// init() blocks registering "generic"; that cannot happen with a
		// single source file, so a silent no-op on the (impossible) error is
		// safe and keeps init() panic-free.
		_ = err
	}
}
