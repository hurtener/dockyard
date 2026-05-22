// This file is the remediation R1 integration test (AGENTS.md §17). The
// pre-Wave-9 depth audit found that the shipping `dockyard inspect` command
// never wired the inspector's Verdicts, Contracts, or App-preview sources:
// every prior inspector test constructed `inspector.Options` directly with
// those fields set, bypassing `runInspect`, so no test exercised the real CLI
// wiring — which is exactly why the gap shipped (Blockers 1 & 2).
//
// This test closes that hole. It drives the REAL `dockyard inspect` binary as
// a subprocess — exactly as a developer runs it — against:
//
//   - a real runtime/server MCP server, served over the real streamable-HTTP
//     transport, with a real runtime/apps ui:// App registered, and a real
//     runtime/obs SSE sink, all behind one HTTP listener so a single --url
//     names both the MCP endpoint and the obs stream;
//   - a real Dockyard project directory (a real dockyard.app.yaml manifest and
//     real generated JSON Schema files) passed via --dir.
//
// It asserts the shipping command wires every source: `/api/verdicts` returns
// a real `dockyard validate` result, `/api/contracts` returns the project's
// generated tool contracts, and `/api/apps` returns the server's ui:// App
// HTML read over a real read-only resources/read. No mock at any seam; runs
// under -race.
package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// r1Input / r1Output are the integration tool contract — a typed Go input and
// output, the contract-first source of truth (P1).
type r1Input struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type r1Output struct {
	Region string `json:"region"`
	Total  int    `json:"total"`
}

func r1Handler(_ context.Context, in r1Input) (tool.Result[r1Output], error) {
	return tool.Result[r1Output]{
		Text:       "region " + in.Region + " reported",
		Structured: r1Output{Region: in.Region, Total: 7},
	}, nil
}

// r1AppHTML is the MCP App the server registers — a ui:// resource the
// inspector reads over resources/read and previews in its App frame.
const r1AppHTML = `<!doctype html><html><head><title>R1 App</title></head>` +
	`<body><div id="app">r1 inspector app</div></body></html>`

// r1Manifest is a real dockyard.app.yaml — the manifest ContractsFromProject
// loads to enumerate the project's tools.
const r1Manifest = `name: r1-inspector
title: R1 Inspector
version: 0.1.0
runtime:
  transports: [http]
tools:
  - name: report
    description: region report
    input: internal/contracts.ReportInput
    output: internal/contracts.ReportOutput
`

// r1InputSchema / r1OutputSchema are real generated JSON Schema files — the
// shape `dockyard generate` writes into internal/contracts/. ContractsFromProject
// reads them from disk; the fixture switcher derives its fixtures from them (P1).
const r1InputSchema = `{"type":"object","properties":{"region":{"type":"string"}},` +
	`"required":["region"]}`

const r1OutputSchema = `{"type":"object","properties":` +
	`{"region":{"type":"string"},"total":{"type":"integer"}}}`

// TestR1_InspectCLIWiresVerdictsContractsAndApps drives the real `dockyard
// inspect` binary against a real HTTP MCP server and a real project directory,
// and asserts the shipping command wires all three previously-unwired sources.
func TestR1_InspectCLIWiresVerdictsContractsAndApps(t *testing.T) {
	// A real obs SSE sink behind the emitter seam.
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// A real server with a real contract-first tool and a real ui:// App.
	srv, err := server.New(
		server.Info{Name: "r1-server", Version: "0.1.0"},
		&server.Options{Obs: sink, Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[r1Input, r1Output]("report").
		Describe("region report").
		Handler(r1Handler)
	if err := report.Register(srv); err != nil {
		t.Fatalf("register report tool: %v", err)
	}
	if err := apps.Register(srv, apps.App{
		URI:  "ui://r1/app",
		Name: "r1-app",
		HTML: []byte(r1AppHTML),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}

	// One HTTP listener fronts both surfaces: /obs/v1/stream is the real obs
	// SSE sink, every other path is the real streamable-HTTP MCP transport. A
	// single --url then names both the obs stream the inspector relays and the
	// MCP endpoint the App-preview path reads ui:// resources from.
	mcpHandler, err := srv.HTTPHandler(&server.HTTPOptions{
		Security: server.DefaultHTTPSecurity(),
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/obs/v1/stream", sink.Handler())
	mux.Handle("/", mcpHandler)

	serverAddr := freeLocalAddr(t)
	httpSrv := &http.Server{Addr: serverAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	httpLn, err := net.Listen("tcp", serverAddr)
	if err != nil {
		t.Fatalf("listen %s: %v", serverAddr, err)
	}
	go func() { _ = httpSrv.Serve(httpLn) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
	if !waitForListener(serverAddr, 10*time.Second) {
		t.Fatalf("combined MCP/obs server did not come up on %s", serverAddr)
	}

	// A real Dockyard project directory: a real manifest and real generated
	// JSON Schema files. ContractsFromProject reads these from disk.
	projectDir := t.TempDir()
	contractsDir := filepath.Join(projectDir, "internal", "contracts")
	if err := os.MkdirAll(contractsDir, 0o750); err != nil {
		t.Fatalf("mkdir contracts dir: %v", err)
	}
	writeFile(t, filepath.Join(projectDir, "dockyard.app.yaml"), r1Manifest)
	writeFile(t, filepath.Join(contractsDir, "report_input.schema.json"), r1InputSchema)
	writeFile(t, filepath.Join(contractsDir, "report_output.schema.json"), r1OutputSchema)

	// Drive the REAL `dockyard inspect` binary as a subprocess — through the
	// real cobra root, runInspect, and inspector.Options wiring.
	inspectorAddr := freeLocalAddr(t)
	inspectorPort := inspectorAddr[strings.LastIndex(inspectorAddr, ":")+1:]

	runCtx, runCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var inspectOut strings.Builder
	go func() {
		defer close(done)
		out, _ := runCLICtx(runCtx, t, projectDir,
			"inspect",
			"--url", "http://"+serverAddr,
			"--dir", projectDir,
			"--port", inspectorPort,
			"--no-open")
		inspectOut.WriteString(out)
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		runCancel()
		<-done
	}
	defer stop()

	if !waitForListener(inspectorAddr, 30*time.Second) {
		stop()
		t.Fatalf("dockyard inspect did not serve on %s\noutput:\n%s",
			inspectorAddr, inspectOut.String())
	}
	base := "http://" + inspectorAddr

	// --- (1) /api/verdicts is wired: a real `dockyard validate` result. -----
	// The project has a manifest but no Go contract source, so validate
	// surfaces real diagnostics — the panel is never an empty void.
	var verdicts []map[string]any
	waitFor(t, func() bool {
		verdicts = decodeJSONArray(t, base+"/api/verdicts")
		return len(verdicts) > 0
	}, "dockyard inspect wires /api/verdicts to a real validate result")

	// --- (2) /api/contracts is wired: the project's generated contracts. ----
	contracts := decodeJSONArray(t, base+"/api/contracts")
	if len(contracts) == 0 {
		stop()
		t.Fatalf("dockyard inspect did not wire /api/contracts — the Fixtures "+
			"switcher would be permanently empty\ninspect output:\n%s", inspectOut.String())
	}
	if name, _ := contracts[0]["name"].(string); name != "report" {
		t.Fatalf("/api/contracts: got tool %q, want \"report\" (contracts not "+
			"sourced from the project manifest)", name)
	}
	if _, ok := contracts[0]["outputSchema"]; !ok {
		t.Fatalf("/api/contracts: the report contract carried no outputSchema — "+
			"the generated schema file was not read: %+v", contracts[0])
	}

	// --- (3) /api/apps is wired: the server's ui:// App, read end to end. ---
	previewApps := decodeJSONArray(t, base+"/api/apps")
	if len(previewApps) == 0 {
		stop()
		t.Fatalf("dockyard inspect did not wire /api/apps — the App-preview "+
			"frame would be permanently empty\ninspect output:\n%s", inspectOut.String())
	}
	html, _ := previewApps[0]["html"].(string)
	if !strings.Contains(html, "r1 inspector app") {
		t.Fatalf("/api/apps did not return the server's ui:// App HTML: %+v", previewApps[0])
	}
	if uri, _ := previewApps[0]["uri"].(string); uri != "ui://r1/app" {
		t.Fatalf("/api/apps: got App URI %q, want \"ui://r1/app\"", uri)
	}

	stop()
}

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// decodeJSONArray GETs url and decodes the response body as a JSON array of
// objects. A non-200 status or a non-array body fails the test.
func decodeJSONArray(t *testing.T, url string) []map[string]any {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx,gosec // test GET against the test's own loopback inspector
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode %s as JSON array: %v", url, err)
	}
	return arr
}
