package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/server"
)

// promptHTTPGet returns the full Response (so the test can check the
// status code) plus the body. The shared httpGet helper in
// inspector_test.go returns only the body — when a test asserts on the
// status code (a 200 vs 502 distinction), it needs the response too.
func promptHTTPGet(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // loopback test URL
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, string(body)
}

// newPromptsTestServer stands up a real runtime/server with one tool and
// three registered prompts, served over the real streamable-HTTP transport
// on a loopback port. The shape mirrors examples/prompts-demo (the demo
// the integration test drives end-to-end) so the unit test exercises the
// same prompts surface a developer hits.
func newPromptsTestServer(t *testing.T) string {
	t.Helper()
	srv, err := server.New(server.Info{Name: "prompts-test", Version: "0.1.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// One tool so the manifest shape is valid against the validate engine
	// (the inspector backend does not run validate; the tool exists for
	// shape parity with the example).
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

	if err := server.AddPrompt(srv, server.PromptDef{
		Name:        "summarize_for_review",
		Title:       "Summarise for engineering review",
		Description: "Two-sentence summary geared at an engineering peer.",
		Arguments: []server.PromptArgument{
			{Name: "passage", Description: "The passage to summarise.", Required: true},
			{Name: "audience", Description: "Audience for the summary."},
		},
	}, func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
		audience := req.Arguments["audience"]
		if audience == "" {
			audience = "an engineering peer"
		}
		return server.PromptResult{
			Messages: []server.PromptMessage{
				{Role: "system", Text: "You are a careful summariser."},
				{Role: "user", Text: "Please summarise the following passage for " + audience + ":\n" + req.Arguments["passage"]},
			},
		}, nil
	}); err != nil {
		t.Fatalf("AddPrompt summarize_for_review: %v", err)
	}
	if err := server.AddPrompt(srv, server.PromptDef{
		Name:        "code_review",
		Description: "Review a diff against a rubric.",
		Arguments: []server.PromptArgument{
			{Name: "diff", Required: true},
			{Name: "language"},
		},
	}, func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
		lang := req.Arguments["language"]
		if lang == "" {
			lang = "Go"
		}
		return server.PromptResult{
			Messages: []server.PromptMessage{
				{Role: "system", Text: "You are a careful code reviewer."},
				{Role: "user", Text: "Review the following " + lang + " diff:\n" + req.Arguments["diff"]},
			},
		}, nil
	}); err != nil {
		t.Fatalf("AddPrompt code_review: %v", err)
	}
	if err := server.AddPrompt(srv, server.PromptDef{
		Name: "explain_error",
		Arguments: []server.PromptArgument{
			{Name: "error", Required: true},
		},
	}, func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
		if req.Arguments["error"] == "" {
			return server.PromptResult{}, errors.New("error is required")
		}
		return server.PromptResult{
			Messages: []server.PromptMessage{
				{Role: "user", Text: "Explain in plain language:\n" + req.Arguments["error"]},
			},
		}, nil
	}); err != nil {
		t.Fatalf("AddPrompt explain_error: %v", err)
	}

	httpHandler, err := srv.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpSrv := &http.Server{Handler: httpHandler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
	return "http://" + ln.Addr().String()
}

// TestPromptsFromServer drives prompts/list + prompts/get end-to-end
// against a real runtime/server — the operator-initiated path D-163
// makes binding.
func TestPromptsFromServer(t *testing.T) {
	t.Parallel()

	t.Run("lists the registered prompts", func(t *testing.T) {
		t.Parallel()
		source, _ := PromptsFromServer(newPromptsTestServer(t))
		prompts, err := source(context.Background())
		if err != nil {
			t.Fatalf("source: %v", err)
		}
		names := map[string]bool{}
		for _, p := range prompts {
			names[p.Name] = true
		}
		for _, want := range []string{"summarize_for_review", "code_review", "explain_error"} {
			if !names[want] {
				t.Errorf("missing prompt %q in %v", want, names)
			}
		}
	})

	t.Run("invokes a real prompts/get and renders messages", func(t *testing.T) {
		t.Parallel()
		_, invoker := PromptsFromServer(newPromptsTestServer(t))
		resp, err := invoker(context.Background(), PromptGetRequest{
			Name:      "summarize_for_review",
			Arguments: map[string]string{"passage": "Dockyard ships v1.1.", "audience": "tech leads"}, //nolint:gosec // G101 false positive — "passage" is a prompt argument key, not a credential
		})
		if err != nil {
			t.Fatalf("invoker: %v", err)
		}
		if len(resp.Messages) != 2 {
			t.Fatalf("messages = %d, want 2: %+v", len(resp.Messages), resp.Messages)
		}
		if resp.Messages[0].Role != "system" {
			t.Errorf("first role = %q, want system", resp.Messages[0].Role)
		}
		if !strings.Contains(resp.Messages[1].Text, "tech leads") {
			t.Errorf("second message did not carry the audience arg: %q", resp.Messages[1].Text)
		}
	})

	t.Run("a handler error surfaces as a typed error", func(t *testing.T) {
		t.Parallel()
		_, invoker := PromptsFromServer(newPromptsTestServer(t))
		_, err := invoker(context.Background(), PromptGetRequest{
			Name:      "explain_error",
			Arguments: map[string]string{"error": ""},
		})
		if err == nil {
			t.Fatal("invoker against an empty arg: want error, got nil")
		}
	})

	t.Run("an unknown prompt is a typed transport-level error", func(t *testing.T) {
		t.Parallel()
		_, invoker := PromptsFromServer(newPromptsTestServer(t))
		if _, err := invoker(context.Background(), PromptGetRequest{
			Name: "no-such-prompt",
		}); err == nil {
			t.Fatal("invoker against an unknown prompt: want error, got nil")
		}
	})

	t.Run("a detached source returns a typed error", func(t *testing.T) {
		t.Parallel()
		source, invoker := PromptsFromServer("")
		if _, err := source(context.Background()); err == nil {
			t.Fatal("source(detached): want error, got nil")
		}
		if _, err := invoker(context.Background(), PromptGetRequest{Name: "x"}); err == nil {
			t.Fatal("invoker(detached): want error, got nil")
		}
	})

	t.Run("an unreachable server is a typed error", func(t *testing.T) {
		t.Parallel()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		dead := "http://" + ln.Addr().String()
		_ = ln.Close()
		source, invoker := PromptsFromServer(dead)
		if _, err := source(context.Background()); err == nil {
			t.Fatal("source(dead): want error, got nil")
		}
		if _, err := invoker(context.Background(), PromptGetRequest{Name: "x"}); err == nil {
			t.Fatal("invoker(dead): want error, got nil")
		}
	})
}

// TestPromptsEndpoints exercises `GET /api/prompts` + `POST /api/prompts/get`
// — the operator-driven surfaces the inspector frontend hits.
func TestPromptsEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("no source yields an empty array", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := promptHTTPGet(t, insp.URL()+"/api/prompts")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
		}
		if strings.TrimSpace(body) != "[]" {
			t.Errorf("body = %q, want []", body)
		}
	})

	t.Run("a source error yields 502 with a typed message", func(t *testing.T) {
		t.Parallel()
		failing := PromptSource(func(context.Context) ([]PromptInfo, error) {
			return nil, errors.New("simulated source failure")
		})
		insp, err := New(Options{Prompts: failing})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := promptHTTPGet(t, insp.URL()+"/api/prompts")
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.StatusCode)
		}
		if !strings.Contains(body, "simulated source failure") {
			t.Errorf("body %q did not carry the typed error", body)
		}
	})

	t.Run("/api/prompts/get with no invoker yields 503", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := httpPost(t, insp.URL()+"/api/prompts/get",
			`{"name":"x","arguments":{}}`)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, "detached") {
			t.Errorf("body %q did not mention detached", body)
		}
	})

	t.Run("/api/prompts/get malformed body yields 400", func(t *testing.T) {
		t.Parallel()
		_, invoker := PromptsFromServer(newPromptsTestServer(t))
		insp, err := New(Options{PromptInvoker: invoker})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, _ := httpPost(t, insp.URL()+"/api/prompts/get", `{not json`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("/api/prompts/get empty name yields 400", func(t *testing.T) {
		t.Parallel()
		_, invoker := PromptsFromServer(newPromptsTestServer(t))
		insp, err := New(Options{PromptInvoker: invoker})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, _ := httpPost(t, insp.URL()+"/api/prompts/get",
			`{"name":"","arguments":{}}`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("a real /api/prompts/get returns rendered messages", func(t *testing.T) {
		t.Parallel()
		source, invoker := PromptsFromServer(newPromptsTestServer(t))
		insp, err := New(Options{Prompts: source, PromptInvoker: invoker})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		// List shows the three demo prompts.
		listResp, listBody := promptHTTPGet(t, insp.URL()+"/api/prompts")
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("/api/prompts status = %d", listResp.StatusCode)
		}
		var listed []PromptInfo
		if err := json.Unmarshal([]byte(listBody), &listed); err != nil {
			t.Fatalf("decode list: %v (body=%s)", err, listBody)
		}
		if len(listed) != 3 {
			t.Errorf("listed %d prompts, want 3: %+v", len(listed), listed)
		}

		// One operator invocation flows through end-to-end.
		getResp, getBody := httpPost(t, insp.URL()+"/api/prompts/get",
			`{"name":"summarize_for_review","arguments":{"passage":"Dockyard ships v1.1.","audience":"reviewers"}}`)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("/api/prompts/get status = %d, body=%s", getResp.StatusCode, getBody)
		}
		var rendered PromptGetResponse
		if err := json.Unmarshal([]byte(getBody), &rendered); err != nil {
			t.Fatalf("decode rendered: %v (body=%s)", err, getBody)
		}
		if len(rendered.Messages) != 2 {
			t.Fatalf("messages = %d, want 2", len(rendered.Messages))
		}
		if !strings.Contains(rendered.Messages[1].Text, "reviewers") {
			t.Errorf("rendered message lacks audience: %q", rendered.Messages[1].Text)
		}
	})

	t.Run("a transport failure yields 502 with a typed message", func(t *testing.T) {
		t.Parallel()
		failing := PromptInvoker(func(context.Context, PromptGetRequest) (*PromptGetResponse, error) {
			return nil, errors.New("simulated transport failure")
		})
		insp, err := New(Options{PromptInvoker: failing})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := httpPost(t, insp.URL()+"/api/prompts/get",
			`{"name":"x","arguments":{}}`)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.StatusCode)
		}
		if !strings.Contains(body, "simulated transport failure") {
			t.Errorf("body %q did not carry the typed error", body)
		}
	})
}
