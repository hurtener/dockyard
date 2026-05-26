package apps

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrUnknownHost is returned (wrapped) when a host id has no registered host
// profile. Callers can match it with errors.Is.
var ErrUnknownHost = errors.New("dockyard/runtime/apps: unknown host profile")

// HostProfile is a pluggable bundle of host-specific *derivation functions* —
// algorithms, never a capability matrix (RFC §7.5, D-011, D-012). A profile
// captures the small, host-specific quirks Dockyard must reproduce without
// hardcoding a host list in the Apps core; today that is one quirk: deriving
// the dedicated sandboxed-iframe origin (`_meta.ui.domain`).
//
// New hosts are added as new driver files that self-register via init(); the
// core never enumerates hosts (brief 01 §4 sharp edge 3, §5).
type HostProfile interface {
	// ID is the stable host identifier the profile registers under (e.g.
	// "claude", "generic"). It must be non-empty and unique in the registry.
	ID() string
	// DeriveDomain derives the dedicated sandboxed-iframe origin for the host
	// from a host-agnostic domain label and the MCP server URL.
	//
	// An empty label means the App declared no dedicated origin; a profile
	// must then return "" with a nil error so the runtime omits
	// `_meta.ui.domain` entirely (preserving Phase 09's deny-by-default
	// omission — RFC §7.4). A non-empty label yields the host's concrete
	// origin form (for Claude, a SHA-256-derived `claudemcpcontent.com`
	// subdomain — brief 01 §2.5).
	DeriveDomain(label, serverURL string) (string, error)
	// RequiresServerURL reports whether the profile cannot derive a domain
	// from a non-empty label without a non-empty serverURL — the case of a
	// signing host that binds the derivation to the server URL so distinct
	// servers cannot forge each other's origin (brief 01 §2.5, D-063, D-064).
	//
	// A pass-through profile (e.g. "generic") returns false: it returns the
	// label verbatim regardless of serverURL. A signing profile (e.g.
	// "claude") returns true: feeding it an empty serverURL yields the
	// ErrInvalidApp-wrapped "cannot derive a signed origin without a server
	// URL" error rather than a forgeable origin.
	//
	// The method is the seam D-165 closes: it lets the
	// capability-degradation testgate category exercise every profile
	// honestly — a profile that requires a server URL is exempt from the
	// empty-URL derivation (its derivation is proven by the profile's own
	// tests, not synthesised in the gate) — without the gate fabricating a
	// synthetic placeholder URL to dodge the invariant.
	RequiresServerURL() bool
}

// hostProfileRegistry is the process-wide interface + factory + driver registry
// of host profiles (AGENTS.md §4.4). It is safe for concurrent use: drivers
// register from init() and lookups happen on every resources/read.
type hostProfileRegistry struct {
	mu       sync.RWMutex
	profiles map[string]HostProfile
}

// hostProfiles is the single process-wide registry instance.
var hostProfiles = &hostProfileRegistry{profiles: make(map[string]HostProfile)}

// RegisterHostProfile installs a host-profile driver in the process-wide
// registry. It is the factory entrypoint of the seam (AGENTS.md §4.4); built-in
// drivers call it from init() and an embedder may call it to add a profile.
//
// It returns a typed error — never panics — on a nil profile, an empty ID, or
// a duplicate ID. Registration is idempotent only in the sense that a profile
// re-registering the *same* ID is rejected; replacing a profile is not allowed,
// so a driver cannot silently shadow another.
func RegisterHostProfile(p HostProfile) error {
	if p == nil {
		return fmt.Errorf("%w: RegisterHostProfile got a nil profile", ErrInvalidApp)
	}
	id := p.ID()
	if id == "" {
		return fmt.Errorf("%w: host profile ID is required", ErrInvalidApp)
	}
	hostProfiles.mu.Lock()
	defer hostProfiles.mu.Unlock()
	if _, dup := hostProfiles.profiles[id]; dup {
		return fmt.Errorf("%w: host profile %q is already registered", ErrInvalidApp, id)
	}
	hostProfiles.profiles[id] = p
	return nil
}

// HostProfileFor returns the registered host profile for id. An empty id
// resolves to the default ("generic") profile, so a caller that has not
// negotiated a host still gets verbatim, spec-faithful behaviour. An
// unregistered id yields a wrapped ErrUnknownHost.
func HostProfileFor(id string) (HostProfile, error) {
	if id == "" {
		return DefaultHostProfile(), nil
	}
	hostProfiles.mu.RLock()
	p, ok := hostProfiles.profiles[id]
	hostProfiles.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %v)", ErrUnknownHost, id, registeredHostIDs())
	}
	return p, nil
}

// DefaultHostProfile returns the verbatim-passthrough "generic" host profile —
// the profile applied when an App selects no host (HostProfile == ""). It is
// always registered (hostprofile_generic.go init()).
func DefaultHostProfile() HostProfile {
	hostProfiles.mu.RLock()
	p := hostProfiles.profiles[genericHostID]
	hostProfiles.mu.RUnlock()
	return p
}

// registeredHostIDs returns the sorted set of registered host ids — used only
// to make ErrUnknownHost messages actionable. Caller may hold no lock.
func registeredHostIDs() []string {
	hostProfiles.mu.RLock()
	ids := make([]string, 0, len(hostProfiles.profiles))
	for id := range hostProfiles.profiles {
		ids = append(ids, id)
	}
	hostProfiles.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

// RegisteredHostIDs returns the sorted set of every registered host-profile id.
// It is the read side of the host-profile seam for callers that need to
// enumerate hosts — `dockyard test`'s capability-degradation category resolves
// every App through every registered profile, proving no host is special-cased
// outside the registry (CLAUDE.md §6 — never a hardcoded host matrix). The
// returned slice is a fresh copy and safe to retain.
func RegisteredHostIDs() []string {
	return registeredHostIDs()
}
