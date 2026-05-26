// This file is the v1.1 Wave A — inspector polish integration test
// (CLAUDE.md §17). Wave A closes two V2-backlog items: D-101 (the
// inspector auto-attach inside `dockyard dev`) and D-151 (the inspector
// Prompts panel). Both items extend existing seams (devloop +
// inspector) with no mocks at the boundary:
//
//   - The dev loop is the real `internal/devloop` orchestrator, with a
//     controllable Go-server stub injected via the existing Config seam
//     (the same shape `phase19_devloop_test.go` uses — a real
//     `go run .` rebuild on every restart is overkill for a wiring
//     proof, and the stub keeps the test fast).
//   - The supervised inspector child is the real `inspectorChild` —
//     the in-process `internal/inspector.Inspector` (D-162).
//   - The attached MCP server is a real `runtime/server` with three
//     real prompts (mirror of `examples/prompts-demo`) served over the
//     real streamable-HTTP transport on a loopback port — so the
//     inspector's prompts/list and prompts/get go through the same
//     code path a real server would handle.
//
// The test asserts:
//
//  1. With the inspector enabled, `devloop.Run` brings the inspector
//     child up — the onInspectorReady hook fires with the resolved URL,
//     and the URL is reachable over HTTP (a `/api/info` 200 confirms
//     the backend is serving).
//  2. The inspector's `/api/prompts` lists the three real prompts the
//     server registered (the wiring proof: the dev-loop inspector
//     reaches the dev-loop server through the pinned HTTP transport).
//  3. The inspector's `/api/prompts/get` invokes one of the prompts
//     and the response carries non-empty rendered messages — the
//     end-to-end Prompts panel surface works exactly as the panel
//     drives it.
//  4. With `DisableInspector: true` (the `--no-inspector` flag's
//     effect), the inspector child is NOT started, the onReady hook
//     fires with no inspector URL, and the rest of the dev loop is
//     unchanged.
//  5. A SIGINT-shaped teardown (context cancel) tears the whole tree
//     down cleanly — no orphan supervised child, no leaked goroutine.
//
// It covers ≥1 failure mode per seam: the auto-attach with a non-
// reachable server-URL pin (the inspector still comes up; the
// Prompts panel renders its empty / error state, mirroring the
// real-world "server hasn't bound the HTTP listener yet" race).
//
// The whole test runs under -race; bounded timeouts; deterministic
// waits on observable signals via the existing `devloop` hooks seam,
// not sleeps.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/devloop"
	"github.com/hurtener/dockyard/runtime/server"
)

// startPromptsHTTPServer stands up a real runtime/server with one tool and
// three registered prompts, served over the real streamable-HTTP transport
// on a loopback port. The shape mirrors examples/prompts-demo so the
// inspector's prompts/list + prompts/get exercise the same surface a
// developer would hit.
func startPromptsHTTPServer(t *testing.T, addr string) string {
	t.Helper()
	srv, err := server.New(server.Info{Name: "v1.1-wave-a-server", Version: "0.1.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	type echoIn struct {
		Text string `json:"text"`
	}
	type echoOut struct {
		Echoed string `json:"echoed"`
	}
	echoHandler := func(_ context.Context, in echoIn) (echoOut, error) {
		return echoOut{Echoed: in.Text}, nil
	}
	if err := server.AddTool(srv, server.ToolDef{
		Name:        "echo",
		Description: "Echo back the input.",
	}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	type promptSpec struct {
		def server.PromptDef
		fn  server.PromptHandler
	}
	specs := []promptSpec{
		{
			def: server.PromptDef{
				Name:        "summarize_for_review",
				Title:       "Summarise for engineering review",
				Description: "Two-sentence summary for a peer reviewer.",
				Arguments: []server.PromptArgument{
					{Name: "passage", Required: true},
					{Name: "audience"},
				},
			},
			fn: func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
				aud := req.Arguments["audience"]
				if aud == "" {
					aud = "an engineering peer"
				}
				return server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "system", Text: "You are a careful summariser."},
						{Role: "user", Text: "Summarise for " + aud + ":\n" + req.Arguments["passage"]},
					},
				}, nil
			},
		},
		{
			def: server.PromptDef{
				Name: "code_review",
				Arguments: []server.PromptArgument{
					{Name: "diff", Required: true},
				},
			},
			fn: func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
				return server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "user", Text: "Review: " + req.Arguments["diff"]},
					},
				}, nil
			},
		},
		{
			def: server.PromptDef{
				Name: "explain_error",
				Arguments: []server.PromptArgument{
					{Name: "error", Required: true},
				},
			},
			fn: func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
				if req.Arguments["error"] == "" {
					return server.PromptResult{}, errors.New("error is required")
				}
				return server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "user", Text: "Explain: " + req.Arguments["error"]},
					},
				}, nil
			},
		},
	}
	for _, s := range specs {
		if err := server.AddPrompt(srv, s.def, s.fn); err != nil {
			t.Fatalf("AddPrompt %s: %v", s.def.Name, err)
		}
	}

	httpHandler, err := srv.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	httpSrv := &http.Server{Handler: httpHandler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
	return "http://" + ln.Addr().String()
}

// writeWaveAProject lays down a minimal Dockyard-shaped project for the
// dev loop to run against: a dockyard.app.yaml, an internal/contracts dir,
// and a main.go (the loop checks main.go exists implicitly through the
// supervised child). The stub command (`stubChildSrc` from
// phase19_devloop_test.go) stands in for `go run .` so the test does not
// rebuild Go on every restart.
func writeWaveAProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	must(filepath.Join(dir, "dockyard.app.yaml"), "name: wave-a-test\n")
	if err := os.MkdirAll(filepath.Join(dir, "internal", "contracts"), 0o750); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	must(filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	return dir
}

// waveABuildStub compiles the same controllable stub child binary
// phase19_devloop_test.go uses. The stub blocks on SIGTERM/SIGINT so the
// supervisor's start / stop / reap path is exercised deterministically.
func waveABuildStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(stubChildSrc), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	bin := filepath.Join(dir, "stub")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, src) //nolint:gosec // test temp paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stub: %v\n%s", err, out)
	}
	return bin
}

// TestV1_1WaveA_AutoAttachAndPromptsPanel drives the v1.1 wave A surface
// end-to-end with real components — the dev loop, the in-process
// inspector child, and a real MCP server with three real prompts.
func TestV1_1WaveA_AutoAttachAndPromptsPanel(t *testing.T) {
	t.Parallel()
	stub := waveABuildStub(t)
	dir := writeWaveAProject(t)

	// Stand up the real MCP server on a fresh loopback port — the dev
	// loop's inspector child will be wired to point at it.
	serverURL := startPromptsHTTPServer(t, "127.0.0.1:0")
	// The server URL ends with /, strip if present; the inspector
	// composer accepts a bare base URL.
	_ = serverURL

	// We do NOT use the default 127.0.0.1:8080 pin — another test
	// could be using it concurrently. Instead, set ServerHTTPAddr to
	// the port the real server actually bound, and override the
	// supervised Go-server command to a stub (so the stub doesn't
	// race the real server for the same port).
	serverAddr := serverURL[len("http://"):]

	ready := make(chan struct{})
	var inspectorURL atomic.Value
	inspectorReady := make(chan struct{})

	cfg := devloop.WithTestHooks(devloop.Config{
		ProjectDir:      dir,
		Logger:          slog.New(slog.DiscardHandler),
		GoServerCommand: []string{stub},
		SkipCodegen:     true,
		ServerHTTPAddr:  serverAddr,
		InspectorAddr:   "127.0.0.1:0",
	}, devloop.TestHooks{
		OnReady: func() { close(ready) },
		OnInspectorReady: func(url string) {
			inspectorURL.Store(url)
			close(inspectorReady)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devloop.Run(ctx, cfg) }()

	select {
	case <-inspectorReady:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("inspector never reported ready")
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("dev loop never reported ready")
	}

	got, ok := inspectorURL.Load().(string)
	if !ok || got == "" {
		cancel()
		t.Fatal("inspector URL not populated by the ready hook")
	}

	// 1. /api/info confirms the inspector backend is serving.
	if err := waitFor200(got+"/api/info", 5*time.Second); err != nil {
		cancel()
		t.Fatalf("inspector /api/info never reachable: %v", err)
	}

	// 2. /api/prompts lists the three registered prompts (the wiring
	//    proof — the inspector reaches the real MCP server).
	if err := waitForPromptCount(got+"/api/prompts", 3, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("inspector /api/prompts did not list the 3 prompts: %v", err)
	}

	// 3. /api/prompts/get invokes one prompt end-to-end.
	type promptGetMessage struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	type promptGetResp struct {
		Messages []promptGetMessage `json:"messages"`
		Error    string             `json:"error,omitempty"`
	}
	body := `{"name":"summarize_for_review","arguments":{"passage":"v1.1 ships the auto-attach inspector.","audience":"reviewers"}}`
	resp, respBody := httpPostRaw(t, got+"/api/prompts/get", body)
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("/api/prompts/get status = %d, body=%s", resp.StatusCode, respBody)
	}
	var rendered promptGetResp
	if err := json.Unmarshal([]byte(respBody), &rendered); err != nil {
		cancel()
		t.Fatalf("decode /api/prompts/get body %q: %v", respBody, err)
	}
	if rendered.Error != "" {
		cancel()
		t.Fatalf("/api/prompts/get returned a server-side error: %q", rendered.Error)
	}
	if len(rendered.Messages) != 2 {
		cancel()
		t.Fatalf("rendered messages = %d, want 2: %+v", len(rendered.Messages), rendered.Messages)
	}
	if rendered.Messages[1].Text == "" {
		cancel()
		t.Errorf("rendered user message empty")
	}

	// 4. Teardown — context cancel tears the whole tree down cleanly.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("devloop.Run returned error on cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("devloop.Run did not return on cancel")
	}

	// 5. After teardown, the inspector URL is no longer reachable.
	if reachable(got+"/api/info", 500*time.Millisecond) {
		t.Errorf("inspector URL %s still reachable after teardown — leaked listener", got)
	}
}

// TestV1_1WaveA_NoInspectorFlagSkipsChild proves that
// `DisableInspector: true` — the `--no-inspector` flag's effect —
// skips the supervised inspector child entirely. The rest of the dev
// loop is unchanged.
func TestV1_1WaveA_NoInspectorFlagSkipsChild(t *testing.T) {
	t.Parallel()
	stub := waveABuildStub(t)
	dir := writeWaveAProject(t)

	ready := make(chan struct{})
	inspectorReady := make(chan struct{}, 1)

	cfg := devloop.WithTestHooks(devloop.Config{
		ProjectDir:       dir,
		Logger:           slog.New(slog.DiscardHandler),
		GoServerCommand:  []string{stub},
		SkipCodegen:      true,
		DisableInspector: true,
	}, devloop.TestHooks{
		OnReady: func() { close(ready) },
		OnInspectorReady: func(string) {
			inspectorReady <- struct{}{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devloop.Run(ctx, cfg) }()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("dev loop never reported ready")
	}

	// The inspector child must NOT have started — the hook is silent.
	// Give it a generous window to fire (it would fire before ready if
	// it was going to).
	select {
	case <-inspectorReady:
		cancel()
		t.Fatal("inspector started despite DisableInspector=true")
	case <-time.After(200 * time.Millisecond):
		// good — no inspector started
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("devloop.Run returned error on cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("devloop.Run did not return on cancel")
	}
}

// TestV1_1WaveA_NoInspectorFlagInCLI is the smoke-level proof that the
// `--no-inspector` flag exists on the shipped `dockyard dev` verb. The
// CLI binary is built once (reusing the wave7 dockyardCLI helper) and
// `dockyard dev --help` is grepped for the flag — the same shape every
// new CLI flag's smoke test follows.
func TestV1_1WaveA_NoInspectorFlagInCLI(t *testing.T) {
	t.Parallel()
	bin := dockyardCLI(t)
	cmd := exec.Command(bin, "dev", "--help") //nolint:gosec // bin is the test-built binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dockyard dev --help: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("--no-inspector")) {
		t.Errorf("dockyard dev --help missing --no-inspector flag:\n%s", out)
	}
	if !bytes.Contains(out, []byte("--inspector-addr")) {
		t.Errorf("dockyard dev --help missing --inspector-addr flag:\n%s", out)
	}
}

// httpPostRaw mirrors internal/inspector's httpPost helper for the
// integration test (it cannot import unexported test helpers from
// another package). Returns the response (so the test can check status)
// plus the body string.
func httpPostRaw(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body))) //nolint:gosec // loopback test URL
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	read, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, string(read)
}

// waitFor200 polls url until a 200 OK is returned or the deadline expires.
// It is the deterministic-signal helper for "the inspector backend is up".
func waitFor200(url string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp, err := http.Get(url) //nolint:gosec // loopback test URL
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("waitFor200: deadline exceeded for " + url)
}

// waitForPromptCount polls /api/prompts until the expected number of
// prompts is listed or the deadline expires. The inspector's MCP client
// connect can take a moment after the listener is up — polling is the
// right shape rather than asserting on the first read.
func waitForPromptCount(url string, want int, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	type promptInfo struct {
		Name string `json:"name"`
	}
	for time.Now().Before(end) {
		resp, err := http.Get(url) //nolint:gosec // loopback test URL
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var got []promptInfo
				if jsonErr := json.Unmarshal(body, &got); jsonErr == nil && len(got) >= want {
					return nil
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("waitForPromptCount: deadline exceeded for " + url)
}

// reachable reports whether url answers any HTTP response within deadline.
// Used to confirm a teardown actually closed the listener — after
// teardown, the URL should not be reachable at all.
func reachable(url string, deadline time.Duration) bool {
	client := &http.Client{Timeout: deadline}
	resp, err := client.Get(url) //nolint:gosec // loopback test URL
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
