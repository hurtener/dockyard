package protocolcodec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file mechanically enforces binding property P3 (AGENTS.md §6, §10, §13):
// internal/protocolcodec is the ONLY package that imports or constructs raw MCP
// extension wire types. The acceptance criterion for Phase 02 requires this be
// enforced "with a test or lint check" — this is that test.
//
// Two checks:
//
//   1. No package outside the seam imports a known raw extension wire-type
//      package (the modelcontextprotocol/go-sdk Apps/Tasks extension packages,
//      or modelcontextprotocol/experimental-ext-tasks).
//
//   2. No Go source outside the seam contains the literal extension `_meta`
//      key strings — hand-constructing those shapes elsewhere is exactly the
//      drift the seam exists to prevent.
//
// Both walk the whole module from the repo root, so the guard covers every
// package phases land later, not just today's tree.

// repoRoot returns the module root, walking up from this test's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd() // internal/protocolcodec
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate module root (go.mod)")
	return ""
}

// seamRelPath is this package's path relative to the module root; sources
// under it are exempt from the boundary checks.
const seamRelPath = "internal/protocolcodec"

// forbiddenImportFragments are import-path substrings that denote a raw MCP
// extension wire-type module. Importing one outside the seam violates P3.
var forbiddenImportFragments = []string{
	"modelcontextprotocol/experimental-ext-tasks",
	"go-sdk/mcp/apps",
	"go-sdk/mcp/tasks",
	"ext-apps",
}

// forbiddenMetaKeys are the literal extension `_meta` key strings. They may
// appear only inside the seam (and inside test files, which exercise it).
var forbiddenMetaKeys = []string{
	"ui/resourceUri",
	"io.modelcontextprotocol/related-task",
	"io.modelcontextprotocol/model-immediate-response",
}

// walkModuleGoFiles invokes fn for every non-seam .go source file in the
// module, skipping vendor/, hidden dirs, and this seam package itself.
func walkModuleGoFiles(t *testing.T, fn func(path string, src []byte, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	root := repoRoot(t)
	seam := filepath.Join(root, filepath.FromSlash(seamRelPath))
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == "node_modules" ||
				(strings.HasPrefix(base, ".") && base != ".") {
				return filepath.SkipDir
			}
			if path == seam {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// path comes from filepath.WalkDir over the module's own source
		// tree — reading every .go file is the whole point of this guard.
		src, readErr := os.ReadFile(path) //nolint:gosec // G304: intentional walk of in-repo sources
		if readErr != nil {
			return readErr
		}
		f, parseErr := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		fn(path, src, f, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// TestNoRawWireTypeImportsOutsideSeam asserts no package other than
// internal/protocolcodec imports a raw MCP extension wire-type module.
func TestNoRawWireTypeImportsOutsideSeam(t *testing.T) {
	walkModuleGoFiles(t, func(path string, _ []byte, file *ast.File, _ *token.FileSet) {
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, frag := range forbiddenImportFragments {
				if strings.Contains(p, frag) {
					t.Errorf("%s imports raw extension wire module %q — "+
						"only internal/protocolcodec may (AGENTS.md §10, P3)", path, p)
				}
			}
		}
	})
}

// TestNoExtensionMetaKeysOutsideSeam asserts no Go source outside the seam
// hand-writes an extension `_meta` key literal.
func TestNoExtensionMetaKeysOutsideSeam(t *testing.T) {
	walkModuleGoFiles(t, func(path string, src []byte, _ *ast.File, _ *token.FileSet) {
		text := string(src)
		for _, key := range forbiddenMetaKeys {
			if strings.Contains(text, key) {
				t.Errorf("%s contains the extension _meta key literal %q — "+
					"extension wire shapes belong only in internal/protocolcodec "+
					"(AGENTS.md §10, §13, P3)", path, key)
			}
		}
	})
}

// TestSeamGuardActuallyScans is a meta-assertion: the guard must have a
// non-empty module to walk, otherwise a silently-broken walker would pass both
// checks vacuously.
func TestSeamGuardActuallyScans(t *testing.T) {
	var count int
	walkModuleGoFiles(t, func(string, []byte, *ast.File, *token.FileSet) { count++ })
	if count == 0 {
		t.Fatal("boundary guard scanned zero files — walker is broken")
	}
}
