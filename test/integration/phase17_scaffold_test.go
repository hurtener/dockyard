// This file is the Phase 17 integration test (CLAUDE.md §17). Phase 17's deps
// name Phase 06 and it consumes internal/codegen and the runtime library, so it
// ships an integration test that exercises the seam with real components and no
// mocks: it runs the real `dockyard new` scaffold, then COMPILES AND VETS the
// scaffolded project with the real Go toolchain — the binding "builds" half of
// the acceptance criterion — and separately drives a real MCP tools/list +
// tools/call session, over a real in-memory transport, against a server built
// from the scaffold's own contract types — the "serves" half.
//
// The build step shells out to `go build` against a temp module that `replace`s
// the Dockyard import at this repo's root, so the scaffolded project compiles
// against the real runtime library exactly as a developer's would. It covers a
// failure mode (a non-empty target directory) and runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// repoRoot returns the Dockyard repository root — three directories up from
// this test file (test/integration/<file>). The scaffolded project's go.mod
// `replace`s the Dockyard import at this path so it builds against the real
// runtime library.
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

// TestPhase17_ScaffoldBuildsAndVets runs the real `dockyard new` scaffold, then
// compiles and vets the scaffolded project with the real Go toolchain — the
// binding "the scaffolded project builds" acceptance criterion (master plan
// Phase 17, RFC §9.1, §10).
func TestPhase17_ScaffoldBuildsAndVets(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	parent := t.TempDir()

	res, err := scaffold.Generate(scaffold.Options{
		Name:            "built-server",
		Dir:             parent,
		DockyardReplace: root,
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}

	// `go mod tidy` resolves the scaffold's transitive dependencies through the
	// replace directive at the real Dockyard checkout.
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in scaffolded project failed: %v\n%s", err, out)
	}

	// `go build ./...` — the "builds" proof. CGo-free, as the shipped artifact
	// must be (CLAUDE.md §5).
	build := exec.CommandContext(context.Background(), "go", "build", "./...")
	build.Dir = res.Dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build of scaffolded project failed — the scaffold does not build:\n%v\n%s", err, out)
	}

	// `go vet ./...` — the scaffold ships vet-clean code.
	vet := exec.CommandContext(context.Background(), "go", "vet", "./...")
	vet.Dir = res.Dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go vet of scaffolded project failed:\n%v\n%s", err, out)
	}

	// `go test ./...` — the scaffold ships a passing contract test.
	gotest := exec.CommandContext(context.Background(), "go", "test", "./...")
	gotest.Dir = res.Dir
	gotest.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := gotest.CombinedOutput(); err != nil {
		t.Fatalf("go test of scaffolded project failed:\n%v\n%s", err, out)
	}
}

// scaffoldGreet is the integration test's in-process re-statement of the
// scaffolded greet handler. It uses the scaffold package's own GreetInput /
// GreetOutput contract types — the same types the scaffold generates the
// schema from — so the MCP session below drives the exact contract the
// scaffolded project ships.
func scaffoldGreet(_ context.Context, in scaffold.GreetInput) (tool.Result[scaffold.GreetOutput], error) {
	greeting := in.Greeting
	if greeting == "" {
		greeting = "Hello"
	}
	msg := greeting + ", " + in.Name + "!"
	return tool.Result[scaffold.GreetOutput]{
		Text:       msg,
		Structured: scaffold.GreetOutput{Message: msg, Length: len([]rune(msg))},
	}, nil
}

// TestPhase17_ScaffoldedServerServes drives a real MCP tools/list + tools/call
// session against a server built from the scaffold's own contract types — the
// "the scaffolded project serves" half of the acceptance criterion. The server
// is served over a real in-memory transport (no mock at the MCP boundary) and a
// real SDK client performs the handshake and the calls.
func TestPhase17_ScaffoldedServerServes(t *testing.T) {
	t.Parallel()
	srv, err := server.New(server.Info{
		Name:    "scaffold-itest",
		Version: "0.1.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// Register the example tool exactly as the scaffolded registerTools does:
	// the contract-first builder over the generated schema (P1).
	if err := tool.New[scaffold.GreetInput, scaffold.GreetOutput]("greet").
		Describe("Greet a person by name.").
		Handler(scaffoldGreet).
		Register(srv); err != nil {
		t.Fatalf("register greet tool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "itest-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// tools/list — the scaffolded server advertises exactly the greet tool.
	listed, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "greet" {
		t.Fatalf("tools/list = %+v, want exactly one tool named greet", listed.Tools)
	}
	// The advertised input schema is the generated one — an object schema.
	if !schemaIsObject(t, listed.Tools[0].InputSchema) {
		t.Errorf("greet input schema is not the generated object schema: %+v", listed.Tools[0].InputSchema)
	}

	// tools/call — a real call against the example tool.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "greet",
		Arguments: scaffold.GreetInput{Name: "Ada", Greeting: "Hi"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tools/call returned IsError: %+v", res.Content)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is %T, want an object", res.StructuredContent)
	}
	if sc["message"] != "Hi, Ada!" {
		t.Errorf("structuredContent.message = %v, want %q", sc["message"], "Hi, Ada!")
	}
}

// schemaIsObject reports whether an MCP tool's advertised input schema (typed
// `any` in the SDK's ListTools result) is a JSON Schema object — the shape the
// contract-first generated schema always carries.
func schemaIsObject(t *testing.T, schema any) bool {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal advertised schema: %v", err)
	}
	var decoded struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode advertised schema: %v", err)
	}
	return decoded.Type == "object"
}

// TestPhase17_ScaffoldRejectsNonEmptyTarget covers the failure mode: `dockyard
// new` never overwrites an existing project (CLAUDE.md §17 — ≥1 failure mode).
func TestPhase17_ScaffoldRejectsNonEmptyTarget(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	if _, err := scaffold.Generate(scaffold.Options{Name: "occupied", Dir: parent}); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	// A second scaffold into the same name must be refused, not overwrite.
	_, err := scaffold.Generate(scaffold.Options{Name: "occupied", Dir: parent})
	if err == nil {
		t.Fatal("second scaffold into an existing project: want an error")
	}
}
