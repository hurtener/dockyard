package generate

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// repoRoot returns the Dockyard repository root, three directories up from this
// test file (internal/generate/<file>). A scaffolded project `replace`s the
// Dockyard import at this path so the ephemeral schema generator's `go run`
// resolves against the real runtime library.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test file path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// scaffoldProject runs the real scaffold and `go mod tidy`, returning the
// project directory — the canonical input for the full generate pipeline.
func scaffoldProject(t *testing.T, name string) string {
	t.Helper()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             t.TempDir(),
		DockyardReplace: repoRoot(t),
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return res.Dir
}

func marshalOwnershipForTest(t *testing.T, ownership artifactOwnership) []byte {
	t.Helper()
	sort.Slice(ownership.Artifacts, func(i, j int) bool { return ownership.Artifacts[i].Path < ownership.Artifacts[j].Path })
	raw, err := json.MarshalIndent(ownership, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func readTestFile(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // Test paths are constructed under controlled temporary project directories.
}

func writeTestFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm) //nolint:gosec // Test paths are constructed under controlled temporary project directories.
}

// TestRun_EndToEnd exercises the full Run pipeline — TypeScript in-process plus
// the ephemeral schema generator `go run` — against a real scaffolded project,
// and asserts the generated files are produced and a rerun is idempotent.
func TestRun_EndToEnd(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "gen-e2e")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The pipeline produces the TypeScript file and both schema files.
	for _, want := range []string{
		TSFileName(),
		SchemaFileName("greet", "input"),
		SchemaFileName("greet", "output"),
	} {
		full := filepath.Join(projectDir, filepath.FromSlash(want))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected generated file %s: %v", want, err)
		}
		found := false
		for _, w := range res.Written {
			if w == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Result.Written does not list %s: %v", want, res.Written)
		}
	}

	// A second run changes nothing — the idempotency guarantee.
	res2, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(res2.Changed) != 0 {
		t.Errorf("second Run changed %d files, want 0 — not idempotent: %v",
			len(res2.Changed), res2.Changed)
	}
}

func TestRun_AnonymousLiteralDashTagsMatchSchemaAndTypeScript(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-anonymous-dash")
	writeFile(t, projectDir, "internal/wire/meta.go", `package wire

type Meta struct {
	ImportedValue string `+"`json:\"importedValue\"`"+`
}
`)
	writeFile(t, projectDir, "internal/contracts/anonymous.go", `package contracts

import "example.com/gen-anonymous-dash/internal/wire"

type LocalMeta struct {
	LocalValue string `+"`json:\"localValue\"`"+`
}

type LocalLiteralDash struct {
	LocalMeta `+"`json:\"-,omitempty\"`"+`
}

type ImportedLiteralDash struct {
	wire.Meta `+"`json:\"-,omitempty\"`"+`
}
`)
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools,
		manifest.Tool{Name: "local_dash", Description: "local dash", Input: "internal/contracts.GreetInput", Output: "internal/contracts.LocalLiteralDash", TaskSupport: manifest.TaskSupportForbidden},
		manifest.Tool{Name: "imported_dash", Description: "imported dash", Input: "internal/contracts.GreetInput", Output: "internal/contracts.ImportedLiteralDash", TaskSupport: manifest.TaskSupportForbidden},
	)

	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	ts := planned[TSFileName()]
	for toolName, typeName := range map[string]string{
		"local_dash":    "LocalLiteralDash",
		"imported_dash": "ImportedLiteralDash",
	} {
		schema, validateErr := codegen.ValidateSchema(planned[SchemaFileName(toolName, "output")], false)
		if validateErr != nil {
			t.Fatalf("validate %s schema: %v", toolName, validateErr)
		}
		if property := schema.Properties["-"]; property == nil || property.Type != "object" && property.Ref == "" {
			t.Fatalf("%s literal dash property = %#v, want object", toolName, property)
		}
		if slices.Contains(schema.Required, "-") {
			t.Fatalf("%s literal dash property is required; omitempty must be preserved", toolName)
		}
		if crossErr := codegen.CrossCheck(schema, typeName, ts); crossErr != nil {
			t.Fatalf("CrossCheck %s: %v\n%s", typeName, crossErr, ts)
		}
	}
	if count := strings.Count(string(ts), "'-'?:"); count != 2 {
		t.Fatalf("literal quoted dash properties = %d, want 2:\n%s", count, ts)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_JSONStringDeclarationsAcrossContractFiles(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-json-string-cross-file")
	writeFile(t, projectDir, "internal/contracts/a_scalar.go", `package contracts

type Scalar int
type Count int
`)
	writeFile(t, projectDir, "internal/contracts/b_pointer.go", `package contracts

type Pointer = *Scalar
`)
	writeFile(t, projectDir, "internal/contracts/c_alias.go", `package contracts

type PointerAlias = Pointer
`)
	writeFile(t, projectDir, "internal/contracts/z_payload.go", `package contracts

type JSONStringPayload struct {
	Value PointerAlias `+"`json:\"value,string\"`"+`
	Count `+"`json:\"count,string\"`"+`
}
`)
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools, manifest.Tool{
		Name:        "json_string",
		Description: "cross-file json string declarations",
		Input:       "internal/contracts.GreetInput",
		Output:      "internal/contracts.JSONStringPayload",
		TaskSupport: manifest.TaskSupportForbidden,
	})

	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	schema, err := codegen.ValidateSchema(planned[SchemaFileName("json_string", "output")], false)
	if err != nil {
		t.Fatalf("ValidateSchema: %v", err)
	}
	if got := schema.Properties["value"]; got == nil || !slices.Contains(got.Types, "null") || !slices.Contains(got.Types, "string") {
		t.Fatalf("pointer alias schema = %#v, want nullable string", got)
	}
	if got := schema.Properties["count"]; got == nil || got.Type != "string" {
		t.Fatalf("tagged anonymous scalar schema = %#v, want string", got)
	}
	ts := planned[TSFileName()]
	if crossErr := codegen.CrossCheck(schema, "JSONStringPayload", ts); crossErr != nil {
		t.Fatalf("CrossCheck: %v\n%s", crossErr, ts)
	}
	for _, want := range []string{"value: string | null;", "count: string;"} {
		if !strings.Contains(string(ts), want) {
			t.Fatalf("generated TypeScript missing %q:\n%s", want, ts)
		}
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestRun_RegeneratesAfterContractChange proves Run picks up a contract-source
// change: after editing a contract struct, the regenerated artifacts differ.
func TestRun_RegeneratesAfterContractChange(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "gen-change")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Add a field to GreetOutput — the generated schema/TS must now change.
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	src, err := os.ReadFile(contractsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read contracts.go: %v", err)
	}
	mutated := injectField(string(src))
	if mutated == string(src) {
		t.Fatal("contract mutation did not apply")
	}
	if err := os.WriteFile(contractsPath, []byte(mutated), 0o600); err != nil { //nolint:gosec // contractsPath is under a test temp dir
		t.Fatalf("write mutated contracts.go: %v", err)
	}

	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Run after contract change: %v", err)
	}
	if len(res.Changed) == 0 {
		t.Error("Run after a contract change reported no changed files")
	}
}

func TestRunRemovesOnlyOwnedOrphanedArtifacts(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-orphans")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	orphanSchemaContent := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$comment":"Code generated by dockyard; DO NOT EDIT.","type":"object"}`)
	orphanSchemaRel := SchemaFileName("removed", "output")
	orphanSchema := filepath.Join(projectDir, filepath.FromSlash(orphanSchemaRel))
	if err := os.WriteFile(orphanSchema, orphanSchemaContent, 0o600); err != nil {
		t.Fatal(err)
	}
	generatedDir := filepath.Join(projectDir, "internal", "unused")
	if err := os.MkdirAll(generatedDir, 0o750); err != nil {
		t.Fatal(err)
	}
	orphanMetadataContent := append(append([]byte(nil), generatedEnumHeader...), []byte("\npackage unused\n")...)
	orphanMetadataRel := "internal/unused/" + enumMetadataFile
	orphanMetadata := filepath.Join(generatedDir, enumMetadataFile)
	if err := os.WriteFile(orphanMetadata, orphanMetadataContent, 0o600); err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	rawOwnership, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(rawOwnership, &ownership); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string][]byte{orphanSchemaRel: orphanSchemaContent, orphanMetadataRel: orphanMetadataContent} {
		sum := sha256.Sum256(content)
		ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: rel, SHA256: fmt.Sprintf("%x", sum)})
	}
	rawOwnership = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, rawOwnership, 0o600); err != nil {
		t.Fatal(err)
	}
	manualDir := filepath.Join(projectDir, "manual")
	if err := os.MkdirAll(manualDir, 0o750); err != nil {
		t.Fatal(err)
	}
	manualMetadata := filepath.Join(manualDir, enumMetadataFile)
	if err := os.WriteFile(manualMetadata, []byte("package manual\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 2 {
		t.Fatalf("removed = %v, want two owned orphans", res.Removed)
	}
	for _, path := range []string{orphanSchema, orphanMetadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned orphan still exists at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(manualMetadata); err != nil {
		t.Fatalf("unmarked same-named file was removed: %v", err)
	}
}

func TestRunDoesNotDeleteNestedProjectMetadata(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-nested-owner")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(projectDir, "examples", "nested")
	if err := os.MkdirAll(filepath.Join(nestedDir, "internal", "contracts"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, manifest.DefaultFilename), []byte("name: nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedMetadata := filepath.Join(nestedDir, "internal", "contracts", enumMetadataFile)
	if err := os.WriteFile(nestedMetadata, append(append([]byte(nil), generatedEnumHeader...), []byte("\npackage contracts\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append(append([]byte(nil), generatedEnumHeader...), []byte("\npackage contracts\n")...))
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{
		Path: "examples/nested/internal/contracts/" + enumMetadataFile, SHA256: fmt.Sprintf("%x", sum),
	})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nestedMetadata); err != nil {
		t.Fatalf("nested project metadata was removed: %v", err)
	}
}

func TestRunRejectsOwnershipRecordOutsideGeneratedNamespaces(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-namespace")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(projectDir, "README.md")
	content, err := readTestFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: "README.md", SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "unsupported path") {
		t.Fatalf("Run error = %v, want unsupported ownership path", err)
	}
	got, err := readTestFile(readme)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("README changed after rejected ownership record: %v", err)
	}
}

func TestCheckOwnershipIndexRequiresCurrentPathAndDigest(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-complete")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	removed := ownership.Artifacts[0]
	ownership.Artifacts = ownership.Artifacts[1:]
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "missing current artifact") {
		t.Fatalf("CheckOwnershipIndex error = %v, want missing artifact", err)
	}
	removed.SHA256 = strings.Repeat("0", 64)
	ownership.Artifacts = append(ownership.Artifacts, removed)
	sort.Slice(ownership.Artifacts, func(i, j int) bool { return ownership.Artifacts[i].Path < ownership.Artifacts[j].Path })
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "stale digest") {
		t.Fatalf("CheckOwnershipIndex error = %v, want stale digest", err)
	}
	raw, err = marshalOwnership(planned)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	removed.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("obsolete")))
	removed.Path = filepath.ToSlash(filepath.Join(ContractsDir, "obsolete_output.schema.json"))
	ownership.Artifacts = append(ownership.Artifacts, removed)
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "obsolete artifact") {
		t.Fatalf("CheckOwnershipIndex error = %v, want obsolete artifact", err)
	}
}

func TestCheckOwnershipIndexRejectsNoncanonicalAndUnknownContent(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-canonical")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	canonical, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(canonical, &ownership); err != nil {
		t.Fatal(err)
	}
	reversed := ownership
	reversed.Artifacts = append([]ownedArtifact(nil), ownership.Artifacts...)
	slices.Reverse(reversed.Artifacts)
	reversedRaw, err := json.MarshalIndent(reversed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	reversedRaw = append(reversedRaw, '\n')

	for name, raw := range map[string][]byte{
		"format":  bytes.ReplaceAll(canonical, []byte("  "), []byte("    ")),
		"order":   reversedRaw,
		"unknown": bytes.Replace(canonical, []byte("{\n"), []byte("{\n  \"hand_edited\": true,\n"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := CheckOwnershipIndex(projectDir, planned); err == nil {
				t.Fatal("hand-edited ownership index passed")
			}
		})
	}
}

func TestRunRejectsNonCanonicalOwnershipPathCasing(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-case-alias")
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	f, err := os.OpenFile(contractsPath, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("\ntype Level string\nconst LevelInfo Level = \"info\"\n")
	if closeErr := f.Close(); writeErr != nil || closeErr != nil {
		t.Fatalf("append enum contract: %v / %v", writeErr, closeErr)
	}
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	generatedDir := filepath.Join(projectDir, "internal", "contracts")
	metadataPath := filepath.Join(generatedDir, enumMetadataFile)
	content, err := readTestFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{
		Path: "INTERNAL/contracts/" + enumMetadataFile, SHA256: fmt.Sprintf("%x", sum),
	})
	if err := os.WriteFile(indexPath, marshalOwnershipForTest(t, ownership), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "non-canonical casing") {
		t.Fatalf("Run error = %v, want casing alias rejection", err)
	}
	got, err := readTestFile(metadataPath)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("canonical metadata changed after alias rejection: %v", err)
	}
}

func TestReadOwnershipRejectsSameFileAliases(t *testing.T) {
	projectDir := t.TempDir()
	firstDir := filepath.Join(projectDir, "internal", "first")
	secondDir := filepath.Join(projectDir, "internal", "second")
	for _, dir := range []string{firstDir, secondDir, filepath.Join(projectDir, ".dockyard")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	content := append(append([]byte(nil), generatedEnumHeader...), []byte("\npackage first\n")...)
	first := filepath.Join(firstDir, enumMetadataFile)
	second := filepath.Join(secondDir, enumMetadataFile)
	if err := os.WriteFile(first, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	sum := sha256.Sum256(content)
	ownership := artifactOwnership{Version: ownershipFileFormat, Artifacts: []ownedArtifact{
		{Path: "internal/first/" + enumMetadataFile, SHA256: fmt.Sprintf("%x", sum)},
		{Path: "internal/second/" + enumMetadataFile, SHA256: fmt.Sprintf("%x", sum)},
	}}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	if err := os.WriteFile(indexPath, marshalOwnershipForTest(t, ownership), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOwnership(projectDir); err == nil || !strings.Contains(err.Error(), "target the same file") {
		t.Fatalf("readOwnership error = %v, want SameFile rejection", err)
	}
}

func TestCheckOwnershipIndexRejectsMissingAndNestedObsoleteRecords(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-extras")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		filepath.ToSlash(filepath.Join(ContractsDir, "missing_output.schema.json")),
		"examples/nested/internal/contracts/" + enumMetadataFile,
	} {
		t.Run(filepath.Base(filepath.Dir(rel))+"-extra", func(t *testing.T) {
			copyOwnership := ownership
			copyOwnership.Artifacts = append(append([]ownedArtifact(nil), ownership.Artifacts...), ownedArtifact{
				Path: rel, SHA256: strings.Repeat("0", 64),
			})
			raw := marshalOwnershipForTest(t, copyOwnership)
			if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rel, "nested") {
				nested := filepath.Join(projectDir, "examples", "nested")
				if err := os.MkdirAll(nested, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(nested, manifest.DefaultFilename), []byte("name: nested\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "obsolete artifact") {
				t.Fatalf("CheckOwnershipIndex error = %v, want obsolete artifact", err)
			}
		})
	}
}

func TestRunDoesNotDeleteOrphanWhenOwnershipCommitFails(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-owner-commit-failure")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	oldArtifacts := make(map[string][]byte)
	for _, rel := range []string{TSFileName(), SchemaFileName("greet", "input"), SchemaFileName("greet", "output")} {
		raw, err := readTestFile(filepath.Join(projectDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		oldArtifacts[rel] = raw
	}
	orphanRel := SchemaFileName("obsolete", "output")
	orphanPath := filepath.Join(projectDir, filepath.FromSlash(orphanRel))
	orphan := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$comment":"Code generated by dockyard; DO NOT EDIT.","type":"object"}`)
	if err := os.WriteFile(orphanPath, orphan, 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(orphan)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: orphanRel, SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	contractSource, err := readTestFile(contractsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractsPath, []byte(injectField(string(contractSource))), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected ownership commit failure")
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeOwnershipCommit: func() error {
			return injected
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("Run error = %v, want injected failure", err)
	}
	if got, err := readTestFile(orphanPath); err != nil || !bytes.Equal(got, orphan) {
		t.Fatalf("orphan was changed before ownership commit: %v", err)
	}
	gotIndex, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotIndex, raw) {
		t.Fatal("ownership index changed despite failed commit")
	}
	for rel, want := range oldArtifacts {
		got, err := readTestFile(filepath.Join(projectDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s was not rolled back after ownership commit failure", rel)
		}
	}
}

func TestRunRollsBackWhenCurrentArtifactChangesBeforeOwnershipCommit(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-current-artifact-race")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	old := make(map[string][]byte)
	for _, rel := range []string{TSFileName(), SchemaFileName("greet", "input"), SchemaFileName("greet", "output"), ownershipFile} {
		raw, err := readTestFile(filepath.Join(projectDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		old[rel] = raw
	}
	contractsPath := filepath.Join(projectDir, ContractsDir, "contracts.go")
	source, err := readTestFile(contractsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractsPath, []byte(injectField(string(source))), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeOwnershipCommit: func() error {
			return os.WriteFile(filepath.Join(projectDir, filepath.FromSlash(TSFileName())), []byte("concurrent edit\n"), 0o600)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "changed during generation") {
		t.Fatalf("Run error = %v, want concurrent artifact change", err)
	}
	for rel, want := range old {
		got, err := readTestFile(filepath.Join(projectDir, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s was not rolled back: %v", rel, err)
		}
	}
}

func TestRunQuarantinesOrphanBeforeVerifyingDeletion(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-orphan-race")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	orphanRel := SchemaFileName("obsolete", "output")
	orphanPath := filepath.Join(projectDir, filepath.FromSlash(orphanRel))
	orphan := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$comment":"Code generated by dockyard; DO NOT EDIT.","type":"object"}`)
	if err := os.WriteFile(orphanPath, orphan, 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(orphan)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: orphanRel, SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	modified := []byte(`{"title":"user edit during generation"}`)
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeOwnershipCommit: func() error {
			return os.WriteFile(orphanPath, modified, 0o600)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "reappeared after quarantine") {
		t.Fatalf("Run error = %v, want post-quarantine path refusal", err)
	}
	got, err := readTestFile(orphanPath)
	if err != nil {
		t.Fatalf("modified orphan was not restored: %v", err)
	}
	if !bytes.Equal(got, modified) {
		t.Fatalf("modified orphan was deleted or replaced: got %s", got)
	}
	gotIndex, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotIndex, raw) {
		t.Fatal("ownership index changed despite pre-commit quarantine failure")
	}
	entries, err := os.ReadDir(filepath.Join(projectDir, ".dockyard", "quarantine"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("original owned bytes must remain detectable in quarantine: entries=%v err=%v", entries, err)
	}
}

func TestRunRechecksQuarantineWhenTransitionalIndexIsAlreadyCurrent(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-orphan-unchanged-index")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools, manifest.Tool{
		Name:        "obsolete",
		Description: "obsolete",
		Input:       "internal/contracts.GreetInput",
		Output:      "internal/contracts.GreetOutput",
		TaskSupport: manifest.TaskSupportForbidden,
	})
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	orphanRel := SchemaFileName("obsolete", "output")
	orphanPath := filepath.Join(projectDir, filepath.FromSlash(orphanRel))
	orphan, err := readTestFile(orphanPath)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	oldIndex, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = m.Tools[:1]
	modified := []byte(`{"title":"concurrent replacement"}`)
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeOwnershipCommit: func() error {
			return os.WriteFile(orphanPath, modified, 0o600)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "reappeared after quarantine") {
		t.Fatalf("Run error = %v, want quarantine recheck", err)
	}
	if got, err := readTestFile(indexPath); err != nil || !bytes.Equal(got, oldIndex) {
		t.Fatalf("unchanged transitional index was not preserved: %v", err)
	}
	if got, err := readTestFile(orphanPath); err != nil || !bytes.Equal(got, modified) {
		t.Fatalf("concurrent replacement changed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(projectDir, ".dockyard", "quarantine"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("original orphan was not retained: entries=%v err=%v", entries, err)
	}
	retained, err := readTestFile(filepath.Join(projectDir, ".dockyard", "quarantine", entries[0].Name()))
	if err != nil || !bytes.Equal(retained, orphan) {
		t.Fatalf("retained orphan bytes changed: %v", err)
	}
}

func TestRunRetainsOwnershipWhenPostCommitCleanupFails(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-cleanup-failure")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	orphanRel := SchemaFileName("obsolete", "output")
	orphanPath := filepath.Join(projectDir, filepath.FromSlash(orphanRel))
	orphan := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$comment":"Code generated by dockyard; DO NOT EDIT.","type":"object"}`)
	if err := os.WriteFile(orphanPath, orphan, 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(orphan)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: orphanRel, SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected quarantine cleanup failure")
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeQuarantineCleanup: func() error {
			return injected
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("Run error = %v, want injected cleanup failure", err)
	}
	if got, err := readTestFile(orphanPath); err != nil || !bytes.Equal(got, orphan) {
		t.Fatalf("orphan was not restored after cleanup failure: %v", err)
	}
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "obsolete artifact") {
		t.Fatalf("cleanup failure became invisible to ownership gate: %v", err)
	}
	res, err := Run(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if !slices.Contains(res.Removed, orphanRel) {
		t.Fatalf("retry removed = %v, want %s", res.Removed, orphanRel)
	}
	if !slices.Contains(res.Changed, ownershipFile) {
		t.Fatalf("retry changed = %v, want final ownership index change", res.Changed)
	}
}

func TestRunRollsBackFinalOwnershipAfterRenameSyncFailure(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-final-owner-sync-failure")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools, manifest.Tool{
		Name:        "obsolete",
		Description: "obsolete",
		Input:       "internal/contracts.GreetInput",
		Output:      "internal/contracts.GreetOutput",
		TaskSupport: manifest.TaskSupportForbidden,
	})
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	orphanRel := SchemaFileName("obsolete", "output")
	orphanPath := filepath.Join(projectDir, filepath.FromSlash(orphanRel))
	m.Tools = m.Tools[:1]
	injected := errors.New("injected final ownership directory sync failure")
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		finalOwnershipDirSync: func(*os.File) error {
			return injected
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("Run error = %v, want injected sync failure", err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan cleanup did not complete before final ownership commit: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(projectDir, ".dockyard", ".dockyard-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("final ownership sync failure left untracked backups: %v", backups)
	}
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err == nil || !strings.Contains(err.Error(), "obsolete artifact") {
		t.Fatalf("rolled-back transitional ownership state is not detectable: %v", err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("retry after final ownership sync failure: %v", err)
	}
	if err := CheckOwnershipIndex(projectDir, planned); err != nil {
		t.Fatalf("ownership index remained stale after retry: %v", err)
	}
}

func TestRunDoesNotDeleteUnownedSchemaOrModifiedOwnedArtifact(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-unowned")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(projectDir, filepath.FromSlash(SchemaFileName("manual", "output")))
	if err := os.WriteFile(unowned, []byte(`{"type":"object","title":"user-owned"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unowned); err != nil {
		t.Fatalf("unowned schema was removed: %v", err)
	}

	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"type":"object"}`)
	sum := sha256.Sum256(content)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: filepath.ToSlash(filepath.Join(ContractsDir, "old_output.schema.json")), SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	modified := filepath.Join(projectDir, ContractsDir, "old_output.schema.json")
	if err := os.WriteFile(modified, []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("Run error = %v, want modified ownership refusal", err)
	}
	if _, err := os.Stat(modified); err != nil {
		t.Fatalf("modified owned artifact was removed: %v", err)
	}
}

func TestRunRepairsMissingOwnershipWithoutDeletingUnownedFiles(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-repair-owner")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(projectDir, filepath.FromSlash(ownershipFile))); err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(projectDir, filepath.FromSlash(SchemaFileName("manual", "output")))
	if err := os.WriteFile(unowned, []byte(`{"title":"user-owned"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("repair missing ownership: %v", err)
	}
	for _, path := range []string{unowned, filepath.Join(projectDir, filepath.FromSlash(ownershipFile))} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s after ownership repair: %v", path, err)
		}
	}
}

func TestRunRejectsSymlinkedOwnershipPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	projectDir := scaffoldProject(t, "gen-symlink-owner")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	content := append(append([]byte(nil), generatedEnumHeader...), []byte("\npackage external\n")...)
	target := filepath.Join(external, enumMetadataFile)
	if err := os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projectDir, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{Path: "linked/" + enumMetadataFile, SHA256: fmt.Sprintf("%x", sum)})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run error = %v, want symlink refusal", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("external target was removed: %v", err)
	}
}

func TestRunRejectsSymlinkedOwnershipIndex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	projectDir := scaffoldProject(t, "gen-symlink-index")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	content, err := readTestFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "ownership.json")
	if err := os.WriteFile(external, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, indexPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run error = %v, want ownership-index symlink refusal", err)
	}
	got, err := readTestFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatal("external ownership-index target was modified")
	}
}

func TestRunRootConfinesArtifactCommitAfterDirectorySwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	projectDir := scaffoldProject(t, "gen-root-artifact-swap")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	contractsSource := filepath.Join(projectDir, ContractsDir, "contracts.go")
	raw, err := readTestFile(contractsSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractsSource, []byte(injectField(string(raw))), 0o600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	marker := filepath.Join(external, contractsTSFile)
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectDir, ContractsDir)
	saved := original + ".saved"
	swapped := false
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeArtifactCommit: func(string) error {
			if swapped {
				return nil
			}
			swapped = true
			if err := os.Rename(original, saved); err != nil {
				return err
			}
			return os.Symlink(external, original)
		},
	})
	if err == nil {
		t.Fatal("Run succeeded after artifact directory escaped through a symlink")
	}
	if got, readErr := readTestFile(marker); readErr != nil || string(got) != "outside" {
		t.Fatalf("external artifact was modified: %q, %v", got, readErr)
	}
	if swapped {
		if err := os.Remove(original); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(saved, original); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunRootConfinesOwnershipCommitAfterDirectorySwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	projectDir := scaffoldProject(t, "gen-root-owner-swap")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	contractsSource := filepath.Join(projectDir, ContractsDir, "contracts.go")
	raw, err := readTestFile(contractsSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(contractsSource, []byte(injectField(string(raw))), 0o600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	marker := filepath.Join(external, "generated-artifacts.json")
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectDir, ".dockyard")
	saved := original + ".saved"
	_, err = Run(Options{
		ProjectDir: projectDir,
		Manifest:   m,
		beforeOwnershipCommit: func() error {
			if err := os.Rename(original, saved); err != nil {
				return err
			}
			return os.Symlink(external, original)
		},
	})
	if err == nil {
		t.Fatal("Run succeeded after ownership directory escaped through a symlink")
	}
	if got, readErr := readTestFile(marker); readErr != nil || string(got) != "outside" {
		t.Fatalf("external ownership file was modified: %q, %v", got, readErr)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, original); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsNonCanonicalOwnershipAlias(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-path-alias")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectDir, filepath.FromSlash(TSFileName()))
	content, err := readTestFile(target)
	if err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(projectDir, filepath.FromSlash(ownershipFile))
	raw, err := readTestFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership artifactOwnership
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	ownership.Artifacts = append(ownership.Artifacts, ownedArtifact{
		Path: "internal/contracts/../contracts/contracts.ts", SHA256: fmt.Sprintf("%x", sum),
	})
	raw = marshalOwnershipForTest(t, ownership)
	if err := os.WriteFile(ownershipPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "invalid generated artifact ownership record") {
		t.Fatalf("Run error = %v, want non-canonical path rejection", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("aliased current artifact was removed: %v", err)
	}
}

func TestRunRefusesToOverwriteUnmarkedEnumMetadata(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-metadata-conflict")
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(projectDir, "internal", "contracts", enumMetadataFile)
	want := []byte("package contracts\n\nvar userOwned = true\n")
	if err := os.WriteFile(metadata, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("Run error = %v, want ownership conflict", err)
	}
	got, err := readTestFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("unmarked metadata changed:\n%s", got)
	}
}

func TestPlanRejectsCanonicalContractCustomJSONEncoding(t *testing.T) {
	projectDir := scaffoldProject(t, "gen-custom-encoding")
	contractsPath := filepath.Join(projectDir, ContractsDir, "contracts.go")
	f, err := os.OpenFile(contractsPath, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("\nfunc (GreetOutput) MarshalJSON() ([]byte, error) { return nil, nil }\n")
	if closeErr := f.Close(); writeErr != nil || closeErr != nil {
		t.Fatalf("append custom encoder: %v / %v", writeErr, closeErr)
	}
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(Options{ProjectDir: projectDir, Manifest: m}); !errors.Is(err, codegen.ErrTypeScriptGen) || !strings.Contains(err.Error(), "custom JSON encoding") {
		t.Fatalf("Plan error = %v, want custom encoding rejection", err)
	}
}

func TestPlan_EnumsRecursiveAndScalarOutput(t *testing.T) {
	t.Parallel()
	projectDir := scaffoldProject(t, "gen-modern-contracts")
	contractsPath := filepath.Join(projectDir, "internal", "contracts", "contracts.go")
	f, err := os.OpenFile(contractsPath, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`
type Severity string
const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
)
type Node struct {
	Level Severity ` + "`json:\"level\"`" + `
	Children []*Node ` + "`json:\"children,omitempty\"`" + `
}
`)
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatalf("append contracts: %v / %v", err, closeErr)
	}
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	m.Tools = append(m.Tools,
		manifest.Tool{Name: "tree", Description: "tree", Input: "internal/contracts.GreetInput", Output: "internal/contracts.Node", TaskSupport: manifest.TaskSupportForbidden},
		manifest.Tool{Name: "severity", Description: "severity", Input: "internal/contracts.GreetInput", Output: "internal/contracts.Severity", TaskSupport: manifest.TaskSupportForbidden},
	)
	planned, err := Plan(Options{ProjectDir: projectDir, Manifest: m})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	tree := string(planned[SchemaFileName("tree", "output")])
	if !strings.Contains(tree, `"$ref": "#/$defs/example.com~1gen-modern-contracts~1internal~1contracts.Node"`) || !strings.Contains(tree, `"enum":`) {
		t.Fatalf("recursive enum schema missing expected graph:\n%s", tree)
	}
	var scalar map[string]any
	if err := json.Unmarshal(planned[SchemaFileName("severity", "output")], &scalar); err != nil {
		t.Fatal(err)
	}
	if scalar["type"] != "string" {
		t.Fatalf("scalar output type = %v", scalar["type"])
	}
	if got := scalar["enum"].([]any); len(got) != 2 {
		t.Fatalf("scalar enum = %v", got)
	}
	metadata := planned["internal/contracts/dockyard_enums.generated.go"]
	if !strings.Contains(string(metadata), `example.com/gen-modern-contracts/internal/contracts.Severity`) {
		t.Fatalf("qualified enum metadata missing:\n%s", metadata)
	}
	if _, err := Run(Options{ProjectDir: projectDir, Manifest: m}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	writeFile(t, projectDir, "enum_parity_test.go", `package main
import (
	"testing"
	"example.com/gen-modern-contracts/internal/contracts"
	"github.com/hurtener/dockyard/runtime/tool"
)
func TestGeneratedEnumRuntimeParity(t *testing.T) {
	s, err := tool.OutputSchemaFor[contracts.Node]()
	if err != nil { t.Fatal(err) }
	if len(s.Properties["level"].Enum) != 2 { t.Fatalf("enum = %v", s.Properties["level"].Enum) }
}
`)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime enum parity: %v\n%s", err, output)
	}
}

// injectField adds an extra field to the GreetOutput struct in scaffolded
// contract source.
func injectField(src string) string {
	const anchor = "type GreetOutput struct {"
	const inject = anchor + "\n\t// Extra is a field added to force regeneration.\n\tExtra string `json:\"extra,omitempty\"`"
	return strings.Replace(src, anchor, inject, 1)
}
