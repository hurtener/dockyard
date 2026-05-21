package apps

import "fmt"

// DerivedDomain is the single choke point through which an App's host-agnostic
// domain label becomes a concrete `_meta.ui.domain` value (RFC §7.5, D-062).
//
// It resolves the host profile for hostProfileID (an empty id selects the
// "generic" verbatim profile), then runs that profile's DeriveDomain over the
// label and MCP server URL. The result is the dedicated sandboxed-iframe origin
// the resources/read response carries.
//
// An empty label yields an empty origin and a nil error: the App declared no
// dedicated origin, so the runtime omits `_meta.ui.domain` entirely and a host
// reads the deny-by-default policy (RFC §7.4). An unregistered hostProfileID
// yields a wrapped ErrUnknownHost.
//
// Routing every derivation through this one function keeps the Apps core free
// of host-specific code: apps.go calls DerivedDomain and never names a host
// (brief 01 §4 sharp edge 3, §5).
func DerivedDomain(hostProfileID, label, serverURL string) (string, error) {
	if label == "" {
		return "", nil
	}
	profile, err := HostProfileFor(hostProfileID)
	if err != nil {
		return "", err
	}
	origin, err := profile.DeriveDomain(label, serverURL)
	if err != nil {
		return "", fmt.Errorf(
			"dockyard/runtime/apps: derive _meta.ui.domain via host profile %q: %w",
			profile.ID(), err)
	}
	return origin, nil
}
