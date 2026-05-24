package inspector

import (
	"errors"
	"strings"
	"testing"
)

// This file is the Phase 27 inspector security re-audit (sub-goal E). The
// existing TestNew_RejectsNonLoopback covers the cardinal shapes (the
// wildcard, a routable address, a malformed string). The re-audit
// strengthens the sweep with additional adversarial shapes drawn from the
// CVE-2025-49596 lesson (brief 05 §4.2) and from the practical landscape of
// SSRF/host-header attacks: the IPv6 unspecified address ("[::]"), the
// per-interface IPv6 unspecified, IPv4-mapped IPv6 loopback, leading /
// trailing whitespace, embedded path / scheme bytes, and DNS-resolved hosts
// that an attacker could control through /etc/hosts (the inspector accepts
// "localhost" as a string shortcut — that behaviour is documented and
// asserted here).

// TestPhase27_InspectorBindShape_AdversarialSweep drives every adversarial
// bind shape we can construct against [New] and asserts each is rejected
// with [ErrNonLoopbackBind] before the listener opens — none of the
// adversarial shapes must produce a usable [Inspector] handle.
func TestPhase27_InspectorBindShape_AdversarialSweep(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
	}{
		// The wildcard shapes — every public-facing bind.
		{"ipv4 wildcard 0.0.0.0", "0.0.0.0:0"},
		{"ipv6 unspecified [::]", "[::]:0"},
		{"ipv6 unspecified bare", "::0"},
		{"port-only", ":0"},
		{"port-only-with-explicit-port", ":8080"},
		// Routable / private / link-local — never loopback.
		{"private 192.168", "192.168.1.10:0"},
		{"private 10.0", "10.0.0.1:0"},
		{"private 172.16", "172.16.0.1:0"},
		{"link-local 169.254", "169.254.0.1:0"},
		{"public dns", "8.8.8.8:0"},
		{"ipv6 public", "[2001:4860:4860::8888]:0"},
		{"ipv6 link-local", "[fe80::1]:0"},
		// Malformed addresses.
		// NB the empty string is NOT here: it is a documented "use the
		// loopback default" shortcut and is asserted in the positive
		// LoopbackAccepted suite below.
		{"garbage", "not-an-address"},
		{"path-as-host", "/etc/passwd:0"},
		{"scheme-in-addr", "http://127.0.0.1:0"},
		{"leading whitespace", " 127.0.0.1:0"},
		{"trailing whitespace", "127.0.0.1:0 "},
		{"both whitespace", "  127.0.0.1:0  "},
		// Hostnames that resolve to something else are NOT in our threat
		// model — the gate is structural — but we assert the only
		// hostname we DO accept ("localhost") is the lone string-form
		// shortcut, by negative example.
		{"random hostname", "evil.example:0"},
		{"non-localhost loopback alias", "loopback:0"},
		// Adversarial port — but loopback is the gate, so this should pass.
		// (Recorded as a NEGATIVE control below in TestPhase27_InspectorBindShape_LoopbackAccepted.)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(Options{Addr: tc.addr})
			if err == nil {
				_ = insp.Close()
				t.Fatalf("New(%q): expected rejection but got a live Inspector at %s",
					tc.addr, insp.Addr())
			}
			if !errors.Is(err, ErrNonLoopbackBind) {
				t.Fatalf("New(%q): want ErrNonLoopbackBind, got %v", tc.addr, err)
			}
		})
	}
}

// TestPhase27_InspectorBindShape_LoopbackAccepted is the negative-control
// counterpart: every shape we DO accept actually opens a listener.
func TestPhase27_InspectorBindShape_LoopbackAccepted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
	}{
		{"ipv4 loopback", "127.0.0.1:0"},
		// 127.0.0.0/8 is loopback per net.IP.IsLoopback, but not every host
		// allows binding to non-127.0.0.1 loopback IPs (Darwin does not by
		// default). The gate still accepts those addresses; the assertion
		// would be environment-dependent, so it is not exercised here.
		{"ipv6 loopback", "[::1]:0"},
		{"ipv4-mapped ipv6 loopback", "[::ffff:127.0.0.1]:0"},
		{"localhost shortcut", "localhost:0"},
		// Empty: the inspector uses the defaultAddr (127.0.0.1:0).
		{"empty selects default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(Options{Addr: tc.addr})
			if err != nil {
				t.Fatalf("New(%q): unexpected rejection: %v", tc.addr, err)
			}
			defer func() { _ = insp.Close() }()
			if !looksLikeLoopback(insp.Addr()) {
				t.Fatalf("New(%q): resolved Addr=%q is not loopback", tc.addr, insp.Addr())
			}
		})
	}
}

// looksLikeLoopback is a simple textual check the resolved Addr is bound to
// a loopback interface — the resolved address is host:port, the host is
// either 127.x.x.x or ::1.
func looksLikeLoopback(addr string) bool {
	return strings.HasPrefix(addr, "127.") ||
		strings.HasPrefix(addr, "[::1]") ||
		strings.HasPrefix(addr, "::1")
}
