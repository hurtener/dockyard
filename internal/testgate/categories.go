package testgate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/validate"
	"github.com/hurtener/dockyard/runtime/apps"
)

// This file holds the five category runners. Each is a pure function of the
// project directory (and, where needed, the loaded manifest) and returns a
// Result — never an error and never a panic, so one failing category cannot
// abort the gate.

// runGoTest runs the project's own Go unit tests (`go test ./...`). It is the
// go-test category: a failing project test is a gating regression (RFC §9.4 —
// "failing contract tests" is a build blocker).
//
// CGO_ENABLED=0 keeps the run consistent with the CGo-free build posture; the
// project's tests are not the framework's -race suite.
func runGoTest(projectDir string) Result {
	res := Result{Category: CategoryGoTest, Gating: true}

	cmd := exec.Command("go", "test", "./...") //nolint:gosec // fixed argv; projectDir is caller-supplied
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		res.Passed = false
		res.Detail = fmt.Sprintf("`go test ./...` failed: %v\n%s",
			err, indent(strings.TrimSpace(out.String())))
		return res
	}
	res.Passed = true
	res.Detail = "`go test ./...` passed"
	return res
}

// runContract is the contract-first assertion (P1, RFC §6). It regenerates the
// project's contract artifacts with internal/generate.Plan — a dry run, no
// project file is touched — and proves the committed JSON Schema and TypeScript
// still match the Go contract structs via internal/codegen.CheckStale. A drift
// means a contract struct changed without `dockyard generate` being rerun: a
// gating regression.
func runContract(projectDir string, m *manifest.Manifest) Result {
	res := Result{Category: CategoryContract, Gating: true}

	planned, err := generate.Plan(generate.Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		res.Passed = false
		res.Detail = fmt.Sprintf("codegen could not run — the contract source does not "+
			"compile or a contract type is invalid: %v", err)
		return res
	}

	var drift []string
	for _, rel := range sortedKeys(planned) {
		full := filepath.Join(projectDir, filepath.FromSlash(rel))
		onDisk, readErr := os.ReadFile(full) //nolint:gosec // path composed from a caller-supplied project dir
		if readErr != nil {
			drift = append(drift, fmt.Sprintf("%s is missing — run `dockyard generate`", rel))
			continue
		}
		if staleErr := codegen.CheckStale(onDisk, planned[rel]); staleErr != nil {
			if errors.Is(staleErr, codegen.ErrStaleGenerated) {
				drift = append(drift,
					fmt.Sprintf("%s is stale — it does not match its Go contract source", rel))
				continue
			}
			drift = append(drift, fmt.Sprintf("stale-codegen check on %s failed: %v", rel, staleErr))
		}
	}
	if len(drift) > 0 {
		sort.Strings(drift)
		res.Passed = false
		res.Detail = "contract regression — run `dockyard generate`:\n" + indent(strings.Join(drift, "\n"))
		return res
	}
	res.Passed = true
	res.Detail = fmt.Sprintf("%d contract artifact(s) match their Go source", len(planned))
	return res
}

// runGolden is the fixture / golden-snapshot category. Dockyard's V1 golden
// snapshots are the generated contract artifacts on disk (the per-tool input
// and output JSON Schemas and contracts.ts): the project checks them in so a
// clone builds without a generate step, and `dockyard test` proves they are
// present, structurally valid, and internally coherent.
//
// Distinct from the contract category (which proves the snapshots match the Go
// *source*), runGolden proves the *snapshots themselves* are usable: every
// expected snapshot file exists, every schema parses and resolves, and the
// schema↔TypeScript pair is internally consistent (codegen.CrossCheck). A
// missing or broken snapshot is a gating regression.
func runGolden(projectDir string, m *manifest.Manifest) Result {
	res := Result{Category: CategoryGolden, Gating: true}

	var problems []string
	tsRel := generate.TSFileName()
	tsPath := filepath.Join(projectDir, filepath.FromSlash(tsRel))
	tsRaw, tsErr := os.ReadFile(tsPath) //nolint:gosec // path composed from a caller-supplied project dir
	if tsErr != nil {
		problems = append(problems,
			fmt.Sprintf("%s is missing — run `dockyard generate`", tsRel))
	}

	checked := 0
	for _, t := range m.Tools {
		for _, side := range []string{"input", "output"} {
			rel := generate.SchemaFileName(t.Name, side)
			full := filepath.Join(projectDir, filepath.FromSlash(rel))
			raw, err := os.ReadFile(full) //nolint:gosec // path composed from a caller-supplied project dir
			if err != nil {
				problems = append(problems,
					fmt.Sprintf("%s is missing — run `dockyard generate`", rel))
				continue
			}
			var s jsonschema.Schema
			if err := json.Unmarshal(raw, &s); err != nil {
				problems = append(problems,
					fmt.Sprintf("%s is not valid JSON Schema: %v", rel, err))
				continue
			}
			if _, err := s.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
				problems = append(problems, fmt.Sprintf("%s does not resolve: %v", rel, err))
				continue
			}
			checked++
			// Cross-check the schema against its TypeScript counterpart — the
			// snapshot pair must be internally coherent. The contract reference
			// "<pkg>.<TypeName>" yields the TS interface name.
			if tsErr == nil {
				if tsName := contractTypeName(t, side); tsName != "" {
					if err := codegen.CrossCheck(&s, tsName, tsRaw); err != nil {
						problems = append(problems,
							fmt.Sprintf("schema/TypeScript snapshot drift for %s %s: %v",
								t.Name, side, err))
					}
				}
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		res.Passed = false
		res.Detail = "golden-snapshot regression:\n" + indent(strings.Join(problems, "\n"))
		return res
	}
	res.Passed = true
	res.Detail = fmt.Sprintf("%d schema snapshot(s) present, valid, and coherent with TypeScript", checked)
	return res
}

// runSpecCompliance is the MCP spec-compliance category. It reuses
// internal/validate.Run — the Phase 18 validation engine — and reports the
// spec-compliance class of its diagnostics. Spec compliance is checked against
// the vendored MCP specs, never a live host (CLAUDE.md §11): validate's
// CheckSpec is exactly that check, so `dockyard test` composes it rather than
// reimplementing the spec checks.
//
// A spec-compliance Blocker is a gating regression. A spec-compliance Warning
// is informational and does not fail the run.
func runSpecCompliance(projectDir string) Result {
	res := Result{Category: CategorySpecCompliance, Gating: true}

	report, err := validate.Run(validate.Options{ProjectDir: projectDir})
	if err != nil {
		res.Passed = false
		res.Detail = fmt.Sprintf("the validation engine could not run: %v", err)
		return res
	}

	var blockers, warnings []string
	for _, d := range report.Diagnostics {
		if d.Check != validate.CheckSpec {
			continue
		}
		switch d.Severity {
		case validate.Blocker:
			blockers = append(blockers, d.Message)
		case validate.Warning:
			warnings = append(warnings, d.Message)
		}
	}

	if len(blockers) > 0 {
		sort.Strings(blockers)
		res.Passed = false
		res.Detail = "spec-compliance violation (checked against the vendored MCP specs):\n" +
			indent(strings.Join(blockers, "\n"))
		return res
	}
	res.Passed = true
	if len(warnings) > 0 {
		res.Detail = fmt.Sprintf("conforms to the vendored MCP specs (%d warning(s))", len(warnings))
	} else {
		res.Detail = "conforms to the vendored MCP specs"
	}
	return res
}

// runCapability is the capability-degradation category (RFC §7.5, §12). It
// exercises the project's manifest constructs across host capability sets and
// asserts the project degrades gracefully — never crashes, never depends on a
// hardcoded host matrix (CLAUDE.md §6).
//
// Two degradation axes are exercised:
//
//  1. Host-profile axis. Every App is resolved through every registered host
//     profile (runtime/apps.HostProfileFor) — the pluggable interface+factory
//     +driver seam, the only legitimate path to host-specific behaviour. A
//     profile that errors or panics deriving an App's origin is a regression;
//     a project that resolves cleanly across all of them has no hardcoded host
//     dependency.
//  2. Apps-negotiated axis. For a host that does NOT negotiate the Apps
//     extension, every UI-bearing tool must still have a model-facing path —
//     a typed output contract — so the tool degrades from "rich UI" to "plain
//     MCP tool" rather than failing. A UI-bearing tool with no output contract
//     would crash such a host.
//
// A project with no Apps (the no-template scaffold) trivially passes: there is
// no UI to degrade.
func runCapability(projectDir string, m *manifest.Manifest) Result {
	res := Result{Category: CategoryCapability, Gating: true}
	_ = projectDir // reserved: a future axis may read on-disk App sources.

	hostIDs := apps.RegisteredHostIDs()
	var problems []string

	for _, a := range m.Apps {
		// Axis 1 — resolve the App through every registered host profile. The
		// label is the App's ui:// URI host component — a stable origin label;
		// the point is that the seam resolves for every host without a panic
		// or an error, proving no host is special-cased outside the registry.
		label := originLabel(a.URI)
		for _, id := range hostIDs {
			profile, err := apps.HostProfileFor(id)
			if err != nil {
				problems = append(problems,
					fmt.Sprintf("host profile %q did not resolve for app %q: %v", id, a.ID, err))
				continue
			}
			// A signing host profile (e.g. Claude — D-063, D-064) refuses to derive
			// a stable signed origin when the App declares a non-empty domain label
			// but the runtime is given an empty serverURL — by design: an empty URL
			// would yield a forgeable origin. The capability category proves the
			// SEAM resolves for every host, not that a real binding is correctly
			// configured; a synthetic placeholder URL satisfies the host profile's
			// derivation invariant so we exercise the seam, not the absence of a
			// server URL. The actual signed-origin binding is a runtime concern
			// proven by `runtime/apps`'s own tests (D-064).
			const syntheticServerURL = "https://capability-test.example/mcp"
			if _, err := profile.DeriveDomain(label, syntheticServerURL); err != nil {
				problems = append(problems, fmt.Sprintf(
					"app %q does not degrade for host %q — DeriveDomain failed: %v",
					a.ID, id, err))
			}
		}
		// An App must declare at least one display mode; a host that does not
		// support a given mode falls back to another. An App with no display
		// mode at all has nothing to degrade *to*.
		if len(a.DisplayModes) == 0 {
			problems = append(problems, fmt.Sprintf(
				"app %q declares no display modes — a host that does not support a "+
					"mode has no fallback (RFC §7.2)", a.ID))
		}
	}

	// Axis 2 — every UI-bearing tool must have a model-facing output contract
	// so it degrades to a plain MCP tool when Apps is not negotiated.
	for _, t := range m.Tools {
		if strings.TrimSpace(t.UI) == "" {
			continue // a plain tool has nothing to degrade.
		}
		if strings.TrimSpace(t.Output) == "" {
			problems = append(problems, fmt.Sprintf(
				"tool %q has a UI but no output contract — it cannot degrade to a "+
					"model-facing result for a host that does not negotiate Apps (RFC §7.5)",
				t.Name))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		res.Passed = false
		res.Detail = "capability-degradation regression:\n" + indent(strings.Join(problems, "\n"))
		return res
	}
	res.Passed = true
	res.Detail = fmt.Sprintf("degrades gracefully across %d host profile(s); "+
		"every UI tool has a model-facing fallback", len(hostIDs))
	return res
}

// contractTypeName returns the TypeScript interface name for a tool's input or
// output contract. The manifest contract reference is "<package>.<TypeName>";
// the TS interface is generated under the bare TypeName.
func contractTypeName(t manifest.Tool, side string) string {
	ref := t.Input
	if side == "output" {
		ref = t.Output
	}
	if i := strings.LastIndex(ref, "."); i >= 0 && i+1 < len(ref) {
		return ref[i+1:]
	}
	return ""
}

// originLabel extracts the host component of a ui:// URI — a stable origin
// label fed to a host profile's DeriveDomain. An empty or non-ui:// URI yields
// "", which a profile reads as "no dedicated origin".
func originLabel(uri string) string {
	rest, ok := strings.CutPrefix(uri, "ui://")
	if !ok {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// sortedKeys returns the keys of m sorted — for deterministic iteration.
func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// indent prefixes every line of s with two spaces, so multi-line category
// detail nests cleanly under its verdict line.
func indent(s string) string {
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
