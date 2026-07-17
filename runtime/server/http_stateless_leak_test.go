package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stacksdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/tasks"
)

func TestStatelessHTTPDoesNotLeakServerReadGoroutines(t *testing.T) {
	for _, mode := range []ProtocolMode{Stateless20260728, Dual} {
		t.Run(fmt.Sprintf("mode_%d", mode), func(t *testing.T) {
			run := func(name string, h http.Handler, client *http.Client) {
				t.Run(name, func(t *testing.T) {
					assertStatelessClientsReleaseServerReads(t, h, client)
				})
			}

			newSDKServer := func() *stacksdk.Server {
				return stacksdk.NewServer(&stacksdk.Implementation{Name: "leak-test", Version: "1.0.0"}, nil)
			}
			newSDKHandler := func(s *stacksdk.Server) http.Handler {
				return stacksdk.NewStreamableHTTPHandler(func(*http.Request) *stacksdk.Server { return s },
					&stacksdk.StreamableHTTPOptions{Stateless: true})
			}

			run("bare_sdk_handler", newSDKHandler(newSDKServer()), nil)

			responseServer := newSDKServer()
			responseServer.AddReceivingMiddleware(responseSemanticsMiddleware(Info{Name: "leak-test", Version: "1.0.0"}, nil))
			run("response_semantics", newSDKHandler(responseServer), nil)

			taskResultServer := newSDKServer()
			taskResultServer.AddReceivingMiddleware(createdTaskResultMiddleware())
			run("task_result_middleware", newSDKHandler(taskResultServer), nil)

			dockyard, err := New(Info{Name: "leak-test", Version: "1.0.0"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			dockyardSDKHandler := http.Handler(stacksdk.NewStreamableHTTPHandler(func(*http.Request) *stacksdk.Server {
				return dockyard.mcp
			}, &stacksdk.StreamableHTTPOptions{Stateless: true, Logger: dockyard.log}))
			run("dockyard_server_bare_sdk_handler", dockyardSDKHandler, nil)

			// Rebuild HTTPHandler's transport middleware cumulatively to identify
			// which layer owns any retained SDK server reader.
			incremental := statelessRequestMiddleware(dockyardSDKHandler)
			run("plus_stateless_request_context", incremental, nil)
			incremental = mcpRequestBodyLimit(incremental)
			run("plus_body_limit", incremental, nil)
			incremental = contentTypeMiddleware(incremental)
			run("plus_content_type", incremental, nil)
			incremental = http.NewCrossOriginProtection().Handler(incremental)
			run("plus_cross_origin", incremental, nil)
			incremental = traceparentMiddleware(incremental)
			run("plus_trace", incremental, nil)

			full, err := dockyard.HTTPHandler(&HTTPOptions{ProtocolMode: mode, Security: DefaultHTTPSecurity()})
			if err != nil {
				t.Fatal(err)
			}
			run("dockyard_handler", full, nil)

			var callbackCalls atomic.Int64
			withCallback, err := dockyard.HTTPHandler(&HTTPOptions{
				ProtocolMode: mode,
				Security:     DefaultHTTPSecurity(),
				ServerForRequest: func(*http.Request) *Server {
					callbackCalls.Add(1)
					return dockyard
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			run("per_request_server_callback", withCallback, nil)
			if callbackCalls.Load() == 0 {
				t.Fatal("ServerForRequest was not called")
			}

			engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), nil)
			if err != nil {
				t.Fatal(err)
			}
			instrumented, err := New(Info{Name: "instrumented", Version: "1"}, &Options{
				Logger: slog.New(slog.DiscardHandler),
				Obs:    obs.NewRingBuffer(128),
				Tasks:  engine,
			})
			if err != nil {
				t.Fatal(err)
			}
			instrumentedHandler, err := instrumented.HTTPHandler(&HTTPOptions{ProtocolMode: mode, Security: DefaultHTTPSecurity()})
			if err != nil {
				t.Fatal(err)
			}
			run("tasks_custom_methods_logger_obs", instrumentedHandler, nil)

			authServer, err := New(Info{Name: "auth", Version: "1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			authOpts := authHTTPOptions()
			authOpts.ProtocolMode = mode
			authHandler, err := authServer.HTTPHandler(authOpts)
			if err != nil {
				t.Fatal(err)
			}
			authClient := &http.Client{Transport: bearerTransport{base: http.DefaultTransport}}
			run("authorization", authHandler, authClient)
		})
	}
}

type bearerTransport struct{ base http.RoundTripper }

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer alice")
	return t.base.RoundTrip(req)
}

func assertStatelessClientsReleaseServerReads(t *testing.T, handler http.Handler, httpClient *http.Client) {
	t.Helper()
	before, _ := serverReadStacks()
	ts := httptest.NewServer(handler)

	const clients = 10
	var wg sync.WaitGroup
	wg.Add(clients)
	for i := range clients {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := stacksdk.NewClient(&stacksdk.Implementation{Name: fmt.Sprintf("client-%d", i), Version: "1"}, nil)
			session, err := client.Connect(ctx, &stacksdk.StreamableClientTransport{
				Endpoint:             ts.URL,
				HTTPClient:           httpClient,
				DisableStandaloneSSE: true,
				MaxRetries:           -1,
			}, nil)
			if err != nil {
				t.Errorf("client %d connect: %v", i, err)
				return
			}
			// Ping currently lacks the modern per-request metadata injection used
			// by the other SDK client methods. Its protocol error is immaterial to
			// the teardown assertion: the request still creates and must release a
			// temporary stateless server session.
			if err := session.Ping(ctx, nil); err != nil && !strings.Contains(err.Error(), "missing or invalid _meta field") {
				t.Errorf("client %d ping: unexpected error: %v", i, err)
			}
			if err := session.Close(); err != nil {
				t.Errorf("client %d close: %v", i, err)
			}
		}()
	}
	wg.Wait()
	ts.CloseClientConnections()
	ts.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		after, stacks := serverReadStacks()
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("SDK server Read goroutines after teardown = %d, before = %d\n%s", after, before, stacks)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func serverReadStacks() (int, string) {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	stacks := buf.String()
	return strings.Count(stacks, "(*streamableServerConn).Read"), stacks
}
