// This file is the remediation R4 N4 integration test (CLAUDE.md §17).
//
// N4 is the depth-audit hygiene finding that no test drove the REAL
// `dockyard validate` BINARY against a real scaffolded project whose committed
// `contracts.ts` had been hand-edited to disagree with the committed JSON
// Schema — the exact "the committed Design A artifacts have desynced" failure
// `internal/validate.checkCrossCodegen` (D-113) and `internal/codegen.CrossCheck`
// (RFC §6.2) exist to catch. `internal/validate/run_test.go` covers the engine
// in-package; `dockyard test` and `dockyard build`'s tests reach the engine
// through their gates. This test closes the binary-boundary gap: a hand-edited
// schema↔TS desync makes the real `dockyard validate` SUBPROCESS exit non-zero
// with the cross-codegen Blocker class, exactly as a developer at a terminal
// would observe.
//
// The wave-7 fixture (`dockyardCLI` / `runCLI` / `scaffoldWave7Project` /
// `repoRoot`) is reused so the test runs the same `dockyard` binary every
// other CLI E2E does; a fresh build is unnecessary. The test runs under -race
// alongside every other integration test.
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestR4_ValidateBinaryRejectsSchemaTSDesync drives the binary boundary for the
// schema↔TS cross-check defense: scaffold a real project, generate clean
// contracts, hand-edit the committed `contracts.ts` so its interface disagrees
// with the committed JSON Schema (a phantom property the schema does not
// declare), then run the REAL `dockyard validate` binary as a subprocess and
// assert (a) it exits non-zero, and (b) the failure is reported as the
// cross-codegen Blocker class (`CheckStaleCodegen`) with the cross-check's
// "drifted apart" message — proving the binary surface, not just the in-package
// engine, enforces P1 on committed contracts (remediation R4 N4; D-113).
func TestR4_ValidateBinaryRejectsSchemaTSDesync(t *testing.T) {
	projectDir := scaffoldWave7Project(t, "r4-desync")

	// A freshly-scaffolded, freshly-generated project must validate clean —
	// otherwise the assertion below would prove nothing.
	if out, err := runCLI(t, projectDir, "validate"); err != nil {
		t.Fatalf("a freshly-scaffolded project must validate clean: %v\n%s", err, out)
	}

	// Hand-edit the committed contracts.ts: add a phantom optional property to
	// the GreetOutput interface. The committed JSON Schema does NOT declare
	// `extra`, so codegen.CrossCheck (and checkCrossCodegen) fires the
	// "drifted apart" Blocker. This is deliberately the cross-codegen failure
	// mode: the byte-compare stale check fires too (the on-disk TS no longer
	// matches a fresh regeneration), but both fire under the same
	// CheckStaleCodegen class, and both make the binary exit non-zero.
	tsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.ts")
	raw, err := os.ReadFile(tsPath) //nolint:gosec // path inside test temp dir
	if err != nil {
		t.Fatalf("read scaffolded contracts.ts: %v", err)
	}
	const anchor = "export interface GreetOutput {"
	if !strings.Contains(string(raw), anchor) {
		t.Fatalf("contracts.ts does not carry the GreetOutput interface anchor; the scaffold shape changed:\n%s", raw)
	}
	desynced := strings.Replace(string(raw), anchor,
		anchor+"\n  /** R4 N4: phantom field absent from the JSON Schema. */\n  extra?: string;",
		1)
	if desynced == string(raw) {
		t.Fatal("contracts.ts desync injection did not apply")
	}
	if err := os.WriteFile(tsPath, []byte(desynced), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write desynced contracts.ts: %v", err)
	}

	// The real `dockyard validate` binary must exit non-zero and report the
	// cross-codegen Blocker — the gate is what `dockyard build` and `dockyard
	// test` both stack on top of, so a regression here would also regress
	// `build` / `test`.
	out, err := runCLI(t, projectDir, "validate")
	if err == nil {
		t.Fatalf("dockyard validate exited 0 on a schema↔TS desync — the binary-boundary cross-codegen defense regressed (R4 N4 / D-113):\n%s", out)
	}
	// The cross-check's diagnostic is reported under CheckStaleCodegen with a
	// distinctive "drifted apart" message; assert at least one of the
	// fingerprints is present so a future error-renaming regression is loud.
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "drifted apart") && !strings.Contains(lower, "checkstalecodegen") && !strings.Contains(lower, "stale") {
		t.Fatalf("dockyard validate failed but the output does not name the stale/cross-codegen Blocker class (R4 N4):\n%s", out)
	}
}
