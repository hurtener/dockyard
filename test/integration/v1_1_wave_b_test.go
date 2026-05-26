// This file is the v1.1 wave B integration test (CLAUDE.md §17). Wave B
// closes two recorded post-V1 deferrals from docs/V2-BACKLOG.md, both
// runtime-side:
//
//   - D-164: scaffold + dockyard run auto-wire of tasks.Engine — the
//     scaffold's main.go now constructs a real tasks.NewInMemoryStore() +
//     tasks.NewEngine(...) and attaches it via server.Options{Tasks: engine}
//     whenever the project's manifest declares any tool with
//     task_support: optional or required.
//   - D-165: Claude signed-origin testgate cleanup — the
//     internal/testgate capability category no longer fabricates a
//     synthetic server URL to dodge the signing-host invariant; it
//     consults the new HostProfile.RequiresServerURL method instead.
//
// This integration test exercises the D-164 end-to-end binding with no
// mocks at the seam (CLAUDE.md §17): a real scaffolded project with the
// auto-wire engaged, compiled with the real Go toolchain against this
// repo's runtime library, then exercised over the real streamable-HTTP
// transport. The initialize handshake carries the injected
// capabilities.tasks block — the wire-level proof the engine is genuinely
// attached. The D-165 half is exercised by its unit + smoke tests; this
// integration covers the scaffold + run path that only an end-to-end test
// can prove. It runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// TestV1_1_WaveB_ScaffoldAutoWiresTasksEngine scaffolds a project from
// options that declare the example tool as task_support: required, then
// compiles the scaffolded project with the real Go toolchain and asserts
// the rendered main.go carries the auto-wire markers. This is the
// end-to-end binding for D-164: the rendered project both ships a manifest
// declaring a task-supporting tool AND a main.go that wires the engine,
// and the project compiles.
func TestV1_1_WaveB_ScaffoldAutoWiresTasksEngine(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	parent := t.TempDir()

	res, err := scaffold.Generate(scaffold.Options{
		Name:                   "wave-b-tasks",
		Dir:                    parent,
		DockyardReplace:        root,
		ExampleToolTaskSupport: manifest.TaskSupportRequired,
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}

	// Assert the rendered manifest carries the task_support: required line.
	manifestBytes, err := os.ReadFile(filepath.Join(res.Dir, "dockyard.app.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifestBytes), "task_support: required") {
		t.Fatalf("scaffolded manifest does not declare task_support: required:\n%s",
			manifestBytes)
	}

	// Assert the rendered main.go carries the engine-wire markers.
	mainBytes, err := os.ReadFile(filepath.Join(res.Dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSrc := string(mainBytes)
	for _, marker := range []string{
		"tasks.NewInMemoryStore",
		"tasks.NewEngine",
		"Tasks: engine",
		"engine.StartSweep",
		"defer engine.StopSweep",
	} {
		if !strings.Contains(mainSrc, marker) {
			t.Errorf("auto-wired main.go is missing marker %q\nsource:\n%s",
				marker, mainSrc)
		}
	}

	// Compile the scaffolded project — the auto-wired code must build
	// against the real runtime library exactly as a developer's would.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	build := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "server"+exeExt()), ".") //nolint:gosec // fixed argv; paths are test temp dirs
	build.Dir = res.Dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build of auto-wired scaffold: %v\n%s", err, out)
	}

	vet := exec.Command("go", "vet", "./...")
	vet.Dir = res.Dir
	vet.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go vet of auto-wired scaffold: %v\n%s", err, out)
	}
}

// TestV1_1_WaveB_AutoWiredServerAdvertisesTasksCapability is the binding
// wire-level assertion for D-164: an in-process server constructed with
// the same shape the auto-wired main.go renders (real runtime/server,
// real tasks.Engine over an in-memory TaskStore, attached via
// server.Options{Tasks: engine}) serves the real streamable-HTTP transport
// and advertises capabilities.tasks during initialize — the binary proof
// the engine is attached and tasks/* are routable.
//
// We construct the server in-process rather than execing the scaffolded
// binary so the test is fast + deterministic; the previous test
// (TestV1_1_WaveB_ScaffoldAutoWiresTasksEngine) covers the
// "scaffolded code actually compiles" half. The two together prove the
// auto-wire's end-to-end binding: the rendered code compiles AND a
// server of the same shape advertises tasks on the wire.
func TestV1_1_WaveB_AutoWiredServerAdvertisesTasksCapability(t *testing.T) {
	t.Parallel()

	// The same engine shape the auto-wired main.go renders: in-memory
	// store, conservative Lifecycle defaults, RequestorIdentifiable off
	// (stdio default — brief 02 §4.5), AdvertiseList off.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{
		RequestorIdentifiable: false,
		AdvertiseList:         false,
		PollInterval:          250,
		Lifecycle: tasks.Lifecycle{
			MaxTTL:                    1 * time.Hour,
			DefaultTTL:                5 * time.Minute,
			PurgeInterval:             30 * time.Second,
			MaxConcurrentPerRequestor: 16,
		},
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	engine.StartSweep(ctx)
	defer engine.StopSweep()

	srv, err := server.New(
		server.Info{Name: "wave-b-auto-wire", Title: "Wave B Auto Wire", Version: "0.0.0"},
		&server.Options{Tasks: engine},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Register a single sync tool so the manifest's declaration shape is
	// honoured (a server with no tools is technically valid, but the
	// auto-wire's purpose is to make tasks/* available alongside a real
	// tool surface). The handler is synchronous: the auto-wire makes the
	// engine AVAILABLE; the developer chooses whether a given handler
	// drives it. Auto-wire-vs-tool-handler is documented in the wave plan.
	type inT struct {
		Name string `json:"name"`
	}
	type outT struct {
		Message string `json:"message"`
	}
	if err := tool.New[inT, outT]("greet").
		Describe("greet").
		Handler(func(_ context.Context, in inT) (tool.Result[outT], error) {
			return tool.Result[outT]{Structured: outT{Message: "hi, " + in.Name}}, nil
		}).
		Register(srv); err != nil {
		t.Fatalf("register greet tool: %v", err)
	}

	handler, err := srv.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()

	// Raw initialize POST — the SDK silently drops the experimental tasks
	// capability on parse (it is outside the SDK's typed
	// ServerCapabilities, RFC §8.2), so the assertion reads the raw wire
	// shape just as the R2 mount test does (test/integration/r2_tasks_mount_test.go).
	initCaps := postInitializeCapabilities(t, httpSrv.URL)
	rawTasks, ok := initCaps["tasks"]
	if !ok {
		t.Fatalf("auto-wired server does NOT advertise capabilities.tasks during initialize — engine attachment broken; caps=%v", initCaps)
	}
	if len(rawTasks) == 0 || string(rawTasks) == "null" {
		t.Fatalf("capabilities.tasks is empty/null: %s", rawTasks)
	}
	// Decode the tasks capability JSON via its wire shape — the codec
	// encodes ToolsCall as the nested requests.tools.call envelope (see
	// internal/protocolcodec.EncodeTasksServerCapability). The R2 test
	// uses the typed codec accessor; we mirror that shape inline so this
	// test stays self-contained.
	var wire struct {
		Cancel   *struct{} `json:"cancel"`
		Requests *struct {
			Tools *struct {
				Call *struct{} `json:"call"`
			} `json:"tools"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(rawTasks, &wire); err != nil {
		t.Fatalf("decode tasks capability JSON: %v (raw %s)", err, rawTasks)
	}
	if wire.Requests == nil || wire.Requests.Tools == nil || wire.Requests.Tools.Call == nil {
		t.Fatalf("tasks capability does not declare requests.tools.call (the toolsCall sub-capability); raw=%s", rawTasks)
	}
	if wire.Cancel == nil {
		// cancel is on for any attached engine — sanity check the rest of
		// the envelope made it across.
		t.Errorf("tasks capability is missing cancel; raw=%s", rawTasks)
	}
}

// exeExt returns ".exe" on windows, "" elsewhere — for the test's go-build
// output path.
func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
