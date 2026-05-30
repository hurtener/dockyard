package apps

import "fmt"

// DerivedDomain resolves a domain label through the host-profile seam (RFC
// §7.5). As of D-176 it is a VERBATIM passthrough: the only built-in profile is
// the "generic" one, which returns the label unchanged — `_meta.ui.domain` is a
// host-supplied value Dockyard never synthesises (D-176, supersedes D-062). The
// resources/read emission path no longer calls it (apps.go carries App.Domain
// verbatim directly); DerivedDomain is retained as the seam's read side for the
// testgate capability category and any direct caller, and for a future
// host-blessed transform registered behind RegisterHostProfile.
//
// It resolves the host profile for hostProfileID (an empty id selects the
// "generic" verbatim profile), then runs that profile's DeriveDomain over the
// label and MCP server URL.
//
// An empty label yields an empty origin and a nil error: the App declared no
// dedicated origin, so the runtime omits `_meta.ui.domain` entirely and a host
// reads the deny-by-default policy (RFC §7.4). An unregistered hostProfileID
// yields a wrapped ErrUnknownHost.
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
