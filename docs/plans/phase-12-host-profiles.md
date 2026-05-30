# Phase 12 — Host profiles + `_meta.ui.domain` derivation

> **⚠️ Superseded in part by D-176 (v1.6 wave A — MCP Apps spec-alignment).**
> The MCP Apps spec makes `_meta.ui.domain` a **host-supplied verbatim** value
> (the host mints it; a server copies it), not a framework-derived one — and the
> derived origin was rejected by Claude Desktop on a local connector. So the
> **server-side auto-derivation** and the **synthesising Claude profile** this
> plan specifies are **retired**: `runtime/apps/hostprofile_claude.go` is removed,
> `apps.resourceMeta` emits `App.Domain` byte-for-byte, and `App.HostProfile` /
> `App.ServerURL` are deprecated no-ops. The pluggable host-profile **seam**
> (interface + factory + driver, with the generic verbatim profile) is **kept**
> for a future host-blessed transform. This plan stands as the record of what
> Phase 12 built; the authority is now
> `docs/plans/v1.6-wave-A-apps-spec-alignment.md`, D-176, and the amended
> RFC §7.5.

## Summary

This phase adds **pluggable host profiles** to `runtime/apps`: small bundles of
host-specific *derivation functions* (algorithms, not a capability matrix) that
live behind an interface + factory + driver seam. It uses them to **auto-derive**
`_meta.ui.domain` — the dedicated sandboxed-iframe origin — including Claude's
SHA-256-derived signed `claudemcpcontent.com` content origin. A developer declares
a stable, host-agnostic domain *label*; Dockyard derives the per-host origin so the
App author never hardcodes a host's quirk and the core carries no host list.

## RFC anchor

- RFC §7.5

## Briefs informing this phase

- brief 01
- brief 05

## Brief findings incorporated

- **brief 01 §2.5** — "`_meta.ui.domain` requests a stable dedicated origin for the
  iframe — needed for APIs that allowlist origins (CORS). Claude derives this origin
  as a SHA-256 hash of the MCP server URL: `<hash32>.claudemcpcontent.com`." This
  phase implements that derivation as the `claude` host profile.
- **brief 01 §4 sharp edge 3** — "Domain signing is host-specific. Claude's
  `<sha256>.claudemcpcontent.com` derivation is a Claude implementation detail, not a
  spec mandate. Dockyard must not hardcode it; it should treat 'dedicated origin'
  abstractly and apply host-specific derivations behind a pluggable host profile."
  The `HostProfile` interface is that seam; the core `apps.go` never names a host.
- **brief 01 §5** — "Pluggable host profiles for host-specific derivations (e.g.
  Claude's signed `claudemcpcontent.com` origin)" is listed under *Must build*; the
  matching *Must avoid* entry is "Hardcoding host-specific behavior (Claude domain
  hashing) into the core." The registry + driver pattern satisfies both.
- **brief 05 §2.2** — host behaviour and feature support "vary" and keep changing; a
  static matrix would always drift. Host profiles are *derivation algorithms* keyed
  by host id, registered via `init()` blank-import, so a new host is a new driver
  file, never a core edit — consistent with the capability-driven posture (D-011).

## Findings I'm departing from (if any)

- **brief 01 §2.8 / §5 / §6 Q-3** frame host divergence as a "per-host **capability
  matrix**" that Dockyard "must model" and that `dockyard validate` consumes. This
  phase deliberately departs: RFC §7.5, AGENTS.md §6 and D-011 settled that Dockyard
  builds **no** capability matrix. Host profiles carry *derivation functions only*
  (a domain-derivation algorithm), never a table of supported features. This is the
  same departure already recorded for Phase 09 in D-049; this phase reaffirms it and
  files D-062. No new contradiction is introduced.

## Goals

- A `HostProfile` interface in `runtime/apps` exposing host-specific derivations,
  starting with dedicated-origin (`_meta.ui.domain`) derivation.
- A process-wide host-profile registry with the interface + factory + driver pattern
  (AGENTS.md §4.4): drivers self-register via `init()`, lookup is by host id, the
  registry is safe under concurrent use.
- A built-in `claude` host profile that derives `<hash>.claudemcpcontent.com` from
  the MCP server URL using SHA-256, matching the form brief 01 §2.5 documents.
- A built-in default (`generic`) host profile that passes the developer's declared
  domain label through verbatim — the behaviour Phase 09 plumbed (D-049).
- `_meta.ui.domain` on the resource-read response is **derived** through the active
  host profile, not carried verbatim from `App.Domain`, with the core `apps.go`
  edit kept minimal and host-name-free.

## Non-goals

- UI-resource auto-discovery / the embed pipeline (Phase 10).
- The Svelte bridge shell / `ui/` `postMessage` dialect (Phase 11).
- A per-host *capability* matrix or feature table (forbidden — D-011, AGENTS.md §6).
- Wiring host-profile *selection* to a live `initialize` handshake — the negotiated
  host identity plumbing is a later concern; this phase ships the seam and an
  explicit selection API, and registers profiles by id.
- Profiles for hosts beyond `claude` and `generic`; new hosts are new driver files.

## Acceptance criteria

- [ ] `_meta.ui.domain` is **auto-derived**: an App declaring a domain label has its
      resource-read `_meta.ui.domain` produced by a host profile's derivation
      function, not copied verbatim.
- [ ] The `claude` host profile produces the correct SHA-256-derived signed-origin
      form — a lowercase-hex hash label on the `claudemcpcontent.com` apex, stable
      for a given server URL + domain label, and a valid DNS hostname.
- [ ] Host profiles register through a clean seam: a `HostProfile` interface, a
      registry with a `RegisterHostProfile` factory entrypoint, and `init()`-time
      driver registration (interface + factory + driver — AGENTS.md §4.4).
- [ ] The `runtime/apps` core (`apps.go`) contains **no hardcoded host list** and no
      host name — the only host-specific code lives in driver files behind the seam.
- [ ] The registry is safe under concurrent use (proven by a `-race` concurrency
      test); an unknown host id yields a typed error, never a panic.
- [ ] An App that declares no domain still produces no `_meta.ui.domain` — derivation
      is skipped, the deny-by-default `_meta.ui` omission of Phase 09 is preserved.

## Files added or changed

- `runtime/apps/hostprofile.go` — new: `HostProfile` interface, the registry,
  `RegisterHostProfile` / `HostProfileFor` / `DefaultHostProfile`, `ErrUnknownHost`.
- `runtime/apps/hostprofile_claude.go` — new: the `claude` driver (SHA-256 origin
  derivation), self-registered via `init()`.
- `runtime/apps/hostprofile_generic.go` — new: the `generic` default driver
  (verbatim pass-through), self-registered via `init()`.
- `runtime/apps/domain.go` — new: `DerivedDomain` — the choke-point that runs a
  profile's derivation over an `App`'s domain label + server URL.
- `runtime/apps/apps.go` — minimal edit: `resourceMeta` routes `App.Domain` through
  the derivation choke-point; new `App.HostProfile` / `App.ServerURL` fields.
- `runtime/apps/hostprofile_test.go`, `domain_test.go`,
  `hostprofile_concurrency_test.go` — new tests.
- `docs/plans/phase-12-host-profiles.md`, `scripts/smoke/phase-12.sh` — new.
- `docs/decisions.md` — D-062, D-063, D-064.
- `docs/glossary.md` — "Dedicated origin", "Domain label", "Host-profile registry".

## Public API surface

```go
// HostProfile is a pluggable bundle of host-specific derivation functions.
type HostProfile interface {
    // ID is the stable host identifier the profile registers under.
    ID() string
    // DeriveDomain derives the dedicated sandboxed-iframe origin for the
    // host from a host-agnostic domain label and the MCP server URL.
    DeriveDomain(label, serverURL string) (string, error)
}

// RegisterHostProfile installs a profile driver in the process-wide registry.
func RegisterHostProfile(p HostProfile) error

// HostProfileFor returns the registered profile for a host id.
func HostProfileFor(id string) (HostProfile, error)

// DefaultHostProfile returns the verbatim-passthrough "generic" profile.
func DefaultHostProfile() HostProfile

// DerivedDomain runs the chosen profile's domain derivation; an empty label
// yields an empty origin (no _meta.ui.domain emitted).
func DerivedDomain(hostProfileID, label, serverURL string) (string, error)

// ErrUnknownHost is returned (wrapped) for an unregistered host id.
var ErrUnknownHost = errors.New(...)

// App gains:
//   HostProfile string  // host id selecting the derivation profile; "" => generic
//   ServerURL   string  // MCP server URL the dedicated origin is derived from
```

## Test plan

- **Unit:** `claude` derivation is deterministic, lowercase-hex, a valid DNS label,
  on the `claudemcpcontent.com` apex, and varies with server URL and domain label;
  `generic` passes the label through; empty label ⇒ empty origin; `HostProfileFor`
  unknown id ⇒ `ErrUnknownHost`; `RegisterHostProfile` rejects a nil profile, an
  empty id, and a duplicate id. `DerivedDomain` end-to-end over both profiles.
- **Integration:** `runtime/apps` is the wiring boundary — a registered App with
  `HostProfile: "claude"` is read over a real in-memory MCP session and its
  resource-read `_meta.ui.domain` is the derived signed origin (real driver on the
  seam, no mocks). An App with no domain label still omits `_meta.ui`.
- **Concurrency / golden:** the registry is exercised by concurrent
  `RegisterHostProfile` / `HostProfileFor` / `DerivedDomain` goroutines under
  `-race`; a golden assertion pins the exact derived origin for a fixed
  (server URL, label) pair so a derivation-algorithm change is caught.

## Smoke script additions

- `runtime/apps` builds CGo-free.
- `runtime/apps` exposes the `HostProfile` interface and `RegisterHostProfile`.
- A `claude` host-profile driver file exists and is `init()`-registered.
- `apps.go` contains no hardcoded host name (`claudemcpcontent`, `"claude"`).
- The phase-12 host-profile + domain-derivation tests pass.
- `_meta.ui.domain` derivation is wired into `resourceMeta`.

## Coverage target

- `runtime/apps` — **80%** (new-package default; the package predates this phase but
  the new files meet 80%).

## Dependencies

- Phase 09 — `runtime/apps` server-side Apps extension (the `_meta.ui` choke point
  this phase makes derived).

## Risks / open questions

- **RFC §18 Q-5** is resolved (auto-derive) — this phase implements the resolution.
- The exact Claude hash form (`hash32` length, full URL vs origin as the hash input)
  is a Claude implementation detail brief 01 §2.5 documents at a summary level. The
  derivation is isolated behind the `claude` driver, so a correction is a one-file
  change; D-063 records the chosen concrete form.
- Parallel phase 10 also edits `runtime/apps`; this phase keeps its surface in new
  files and makes the smallest possible `apps.go` edit. A merge conflict in
  `runtime/apps`, `docs/decisions.md`, `docs/glossary.md` is expected and reconciled
  at merge time.

## Glossary additions

- **Dedicated origin** — the stable, per-App sandboxed-iframe origin a host serves
  the App's HTML from (`_meta.ui.domain`), needed for CORS allowlisting.
- **Domain label** — the host-agnostic domain identifier an App author declares;
  Dockyard derives the per-host dedicated origin from it.
- **Host-profile registry** — the process-wide interface + factory + driver registry
  of `HostProfile` derivation drivers.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`
