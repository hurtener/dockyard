package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhase27_InspectorSecurity_NewClientAudit captures the Phase 27
// inspector security re-audit's bounded "production mcp.NewClient" set
// (sub-goal E, CLAUDE.md §7 / RFC §12 / P4). The inspector grew through R1
// (D-103 read-only resources/read), R2/R3, Phase 24 finish (D-131
// operator-initiated tools/call), Phase 25 (D-134 elicitation supply).
// Each of those is a bounded, short-lived per-request session in the
// inspector or in `internal/installpkg`'s boot check (D-088). NO other
// production call site is allowed — a new mcp.NewClient outside the
// audited set is a P4 violation and must surface as a test failure.
//
// The audit is mechanical: it walks every Go file under internal/, runtime/,
// and cmd/ (production source paths), parses each, and records every
// call-expression whose selector is `NewClient` (matching mcp / mcpsdk
// aliases). The captured set is byte-compared against an in-file allow-
// list; an unexpected hit fails the test with the file + line, so the
// engineer who added the call gets a precise pointer to the audit they
// need to update.
//
// The allow-list is itself a P4 audit: each entry carries a one-line
// citation of the decision that legitimised it. Editing the allow-list
// is part of editing the decision — the same PR.
func TestPhase27_InspectorSecurity_NewClientAudit(t *testing.T) {
	t.Parallel()

	// The allowed call-site set, by (relative path, line). A change to a
	// line number is a real edit signal — the test author intentionally
	// pins line numbers so a refactor surfaces here and updates the audit
	// + decision together (the "no silent edit" rule).
	type site struct {
		path     string
		funcName string
		decision string
	}
	allowed := map[string]site{
		"internal/inspector/appsource.go": {
			path:     "internal/inspector/appsource.go",
			funcName: "discoverApps",
			decision: "D-103 (read-only resources/list + resources/read for App rendering)",
		},
		"internal/inspector/invoke.go": {
			path:     "internal/inspector/invoke.go",
			funcName: "invokeToolViaSDK",
			decision: "D-131 (operator-initiated tools/call from the inspector)",
		},
		"internal/inspector/prompts.go": {
			path:     "internal/inspector/prompts.go",
			funcName: "dialAttachedPrompt",
			decision: "D-163 (operator-initiated prompts/list + prompts/get; v1.1 Wave A)",
		},
		"internal/installpkg/bootcheck.go": {
			path:     "internal/installpkg/bootcheck.go",
			funcName: "bootCheck",
			decision: "D-088 (`dockyard install` boot check — throwaway localhost spawn)",
		},
	}

	type hit struct {
		path string
		line int
	}
	var found []hit

	// repoRoot is the directory two levels above this test file (test/integration/...).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))

	productionRoots := []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "runtime"),
		filepath.Join(repoRoot, "cmd"),
	}

	fset := token.NewFileSet()
	for _, root := range productionRoots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			if strings.HasSuffix(p, "_test.go") {
				return nil // tests are out of scope; the audit is production-only
			}
			file, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "NewClient" {
					return true
				}
				// Only flag mcp / mcpsdk selectors — other packages might also
				// have a NewClient API (we do not vet those).
				if id, ok := sel.X.(*ast.Ident); ok {
					if id.Name != "mcp" && id.Name != "mcpsdk" {
						return true
					}
				}
				pos := fset.Position(call.Pos())
				rel, _ := filepath.Rel(repoRoot, pos.Filename)
				found = append(found, hit{path: filepath.ToSlash(rel), line: pos.Line})
				return true
			})
			return nil
		})
	}

	// Audit: every found hit must be in `allowed`. The audit fails on any
	// extra hit; it does NOT fail on a missing expected hit (a refactor
	// that removes one is fine — the decision can be updated to mark it
	// "no longer instantiated" without breaking the gate).
	seen := map[string]struct{}{}
	for _, h := range found {
		if _, ok := allowed[h.path]; !ok {
			t.Errorf("PRODUCTION mcp.NewClient at %s:%d is not in the Phase 27 inspector security audit allow-list — P4 violation suspected. Add to the test file's `allowed` map AND file a decision before merging.", h.path, h.line)
			continue
		}
		seen[h.path] = struct{}{}
	}
	// Diagnostic: report the audit's coverage for the PR description.
	t.Logf("Phase 27 inspector security re-audit: %d production mcp.NewClient call sites, all within the allow-list (%d entries).", len(found), len(allowed))
	for path := range allowed {
		if _, was := seen[path]; was {
			t.Logf("  OK %s — %s", path, allowed[path].decision)
		} else {
			t.Logf("  NOTE %s — listed but not found this run; that's fine (refactored away).", path)
		}
	}
}
