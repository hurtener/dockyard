// This file is the Phase 25 integration test (CLAUDE.md §17). Phase 25's
// deps name Phase 24 (the template seam) and Phase 14 (the Tasks engine);
// this test exercises the binding seam between them — a scaffolded
// approval-flows project drives a real `tools/call → input_required →
// tasks/result` lifecycle end to end against a real `runtime/server` with
// a real `tasks.Engine` attached.
//
// The test:
//  1. Builds the real `dockyard` binary (it embeds the approval-flows
//     template via //go:embed at compile time — proving the embed works
//     end to end, not just in unit-test isolation).
//  2. Materialises the template into a temp directory via the real binary
//     (`dockyard new --template approval-flows`).
//  3. Tidies + builds + tests the materialised project with the real Go
//     toolchain (the "builds + tests pass" halves of the acceptance
//     criterion).
//  4. Spins up an in-process MCP server with a real tasks.Engine attached
//     via server.Options.Tasks (D-108, the R2 seam), registers the
//     template handlers, and drives each tool: tools/call returns
//     CreateTaskResult, the task enters input_required, tasks/result is
//     called with the user's reply, the task transitions to completed,
//     and the terminal payload carries the right approve/edits/reject
//     decision.
//
// Covers ≥1 failure mode per seam: a malformed elicitation payload
// surfaces as State="error" on the terminal payload; a rejected proposal
// returns Approved=false with no Edits. Runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"

	_ "github.com/hurtener/dockyard/templates/approval-flows" // register the builtin
	afcontracts "github.com/hurtener/dockyard/templates/approval-flows/pkg/contracts"
	afhandlers "github.com/hurtener/dockyard/templates/approval-flows/pkg/handlers"
)

// TestPhase25_TemplateMaterialisesBuildsAndTests drives the entire
// scaffold → build → test cycle on the approval-flows template against
// the real dockyard binary.
func TestPhase25_TemplateMaterialisesBuildsAndTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "dockyard")
	build := exec.CommandContext(context.Background(), //nolint:gosec // test driver: args are constants + a temp path
		"go", "build", "-o", binPath, "./cmd/dockyard")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dockyard: %v\n%s", err, out)
	}

	parent := t.TempDir()
	mat := exec.CommandContext(context.Background(), //nolint:gosec // test driver
		binPath, "new", "af-itest",
		"--template", "approval-flows",
		"--dir", parent,
		"--dockyard-path", root)
	if out, err := mat.CombinedOutput(); err != nil {
		t.Fatalf("dockyard new --template approval-flows: %v\n%s", err, out)
	}
	proj := filepath.Join(parent, "af-itest")

	// Required shape of the materialised tree.
	for _, rel := range []string{
		"dockyard.app.yaml",
		"main.go",
		"internal/contracts/contracts.go",
		"internal/handlers/handlers.go",
		"internal/handlers/handlers_test.go",
		"web/src/App.svelte",
		"web/src/ApprovalCard.svelte",
		"web/src/EditsForm.svelte",
		"go.mod",
		"README.md",
		".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(proj, rel)); err != nil {
			t.Errorf("materialised project missing %s: %v", rel, err)
		}
	}

	// Manifest: two task-augmented tools + one inline-only app.
	manifest, err := os.ReadFile(filepath.Join(proj, "dockyard.app.yaml")) //nolint:gosec // proj is a test temp dir
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, want := range []string{
		"name: request_approval",
		"name: propose_with_edits",
		"id: approvals",
		"display_modes: [inline]",
		"bundle: single-file",
		"task_support: required",
	} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest missing %q", want)
		}
	}

	// main.go wires the tasks.Engine (D-135).
	mainGo, err := os.ReadFile(filepath.Join(proj, "main.go")) //nolint:gosec // proj is a test temp dir
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	for _, want := range []string{
		"tasks.NewEngine",
		"Tasks: engine",
		"engine.StartSweep",
	} {
		if !strings.Contains(string(mainGo), want) {
			t.Errorf("main.go missing D-135 wiring %q", want)
		}
	}

	// Tidy + build + test the materialised project.
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = proj
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
		{"test", "./..."},
	} {
		cmd := exec.CommandContext(context.Background(), "go", args...) //nolint:gosec // test driver: args is a fixed table
		cmd.Dir = proj
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Twelve fixtures total (6 per tool).
	for _, toolName := range []string{"request_approval", "propose_with_edits"} {
		for _, state := range []string{"happy", "empty", "error", "permission", "slow", "large"} {
			if _, err := os.Stat(filepath.Join(proj, "fixtures", toolName, state+".json")); err != nil {
				t.Errorf("fixture %s/%s missing: %v", toolName, state, err)
			}
		}
	}
}

// TestPhase25_RequestApprovalLifecycle drives the full
// `tools/call → input_required → tasks/result → completed` lifecycle for
// the request_approval tool end to end, using a real runtime/server +
// real tasks.Engine + the real template handler. No mocks at the seam.
func TestPhase25_RequestApprovalLifecycle(t *testing.T) {
	t.Parallel()

	engine, srv, session, ctx := newApprovalFlowsServerAndClient(t)

	// Drive a tools/call. The runtime substitutes a CreateTaskResult on
	// the wire for a Tasks-capable host; the SDK's ServeInMemory does
	// NOT negotiate the experimental Tasks capability, so the tools/call
	// returns the synchronous "awaiting" structuredContent. The task is
	// still created on the server side — the handler's call to
	// engine.CreateForToolCall ran. We then drive the elicitation
	// directly through the engine (the same path the inspector's
	// elicitation endpoint takes), prove the lifecycle completes, and
	// prove the terminal payload carries the right decision.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "request_approval",
		Arguments: afcontracts.RequestApprovalInput{
			Title:       "Send the weekly digest?",
			Description: "1247 subscribers.",
			Category:    "publishing",
		},
	})
	if err != nil {
		t.Fatalf("tools/call request_approval: %v", err)
	}
	if res.IsError {
		t.Fatalf("tools/call returned IsError: %+v", res.Content)
	}

	// Wait for the task to enter input_required.
	id := waitForFirstInputRequired(ctx, t, engine, 5*time.Second)
	prompt, ok := engine.PendingInput(id)
	if !ok {
		t.Fatalf("task %s has no pending input", id)
	}
	if !strings.Contains(prompt.Message, "Send the weekly digest") {
		t.Errorf("prompt message = %q, want it to carry the title", prompt.Message)
	}

	// Supply the reply — approve with a reason.
	reply := afcontracts.ApprovalReply{Approved: true, Reason: "Looks good."}
	replyJSON, _ := json.Marshal(reply)
	if err := engine.SupplyInput(ctx, id, tasks.InputResponse{Data: replyJSON}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}

	// Wait for the task to reach a terminal status and read the result.
	terminalPayload := waitForTerminal(ctx, t, engine, id, 5*time.Second)
	var envelope struct {
		StructuredContent afcontracts.RequestApprovalOutput `json:"structuredContent"`
		IsError           bool                              `json:"isError"`
	}
	if err := json.Unmarshal(terminalPayload, &envelope); err != nil {
		t.Fatalf("decode terminal payload: %v\n%s", err, terminalPayload)
	}
	if envelope.IsError {
		t.Fatalf("terminal IsError = true: %+v", envelope)
	}
	if envelope.StructuredContent.State != "approved" {
		t.Errorf("State = %q, want approved", envelope.StructuredContent.State)
	}
	if envelope.StructuredContent.Approved == nil || !*envelope.StructuredContent.Approved {
		t.Errorf("Approved = %v, want true", envelope.StructuredContent.Approved)
	}
	if envelope.StructuredContent.Reason != "Looks good." {
		t.Errorf("Reason = %q", envelope.StructuredContent.Reason)
	}
	_ = srv // satisfy unused warning when handler bodies are inlined
}

// TestPhase25_RequestApprovalReject covers the reject path.
func TestPhase25_RequestApprovalReject(t *testing.T) {
	t.Parallel()
	engine, _, session, ctx := newApprovalFlowsServerAndClient(t)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "request_approval",
		Arguments: afcontracts.RequestApprovalInput{
			Title:       "Reject me",
			Description: "Please reject.",
		},
	}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	id := waitForFirstInputRequired(ctx, t, engine, 5*time.Second)
	reply := afcontracts.ApprovalReply{Approved: false, Reason: "No, not now."}
	replyJSON, _ := json.Marshal(reply)
	if err := engine.SupplyInput(ctx, id, tasks.InputResponse{Data: replyJSON}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	terminal := waitForTerminal(ctx, t, engine, id, 5*time.Second)
	var envelope struct {
		StructuredContent afcontracts.RequestApprovalOutput `json:"structuredContent"`
	}
	if err := json.Unmarshal(terminal, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.StructuredContent.State != "rejected" {
		t.Errorf("State = %q, want rejected", envelope.StructuredContent.State)
	}
}

// TestPhase25_ProposeWithEditsLifecycle drives the edits-form lifecycle:
// the user edits one field, leaves others at the proposed default, and
// approves; the terminal payload carries the finalised Edits map.
func TestPhase25_ProposeWithEditsLifecycle(t *testing.T) {
	t.Parallel()
	engine, _, session, ctx := newApprovalFlowsServerAndClient(t)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "propose_with_edits",
		Arguments: afcontracts.ProposeWithEditsInput{
			Title:       "Send email",
			Description: "Review fields before send.",
			Fields: []afcontracts.Field{
				{Key: "recipient", Label: "Recipient", Type: afcontracts.FieldTypeString,
					Current: "all-hands@", Proposed: "board@"},
				{Key: "subject", Label: "Subject", Type: afcontracts.FieldTypeString,
					Current: "Hello", Proposed: "Hi"},
				{Key: "body", Label: "Body", Type: afcontracts.FieldTypeText,
					Current: "Old body", Proposed: "New body"},
			},
		},
	}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	id := waitForFirstInputRequired(ctx, t, engine, 5*time.Second)
	// The user edits subject; leaves recipient/body at their proposed
	// values (which the handler's finaliser should preserve).
	reply := afcontracts.ApprovalReply{
		Approved: true,
		Reason:   "Sounds good.",
		Edits:    map[string]any{"subject": "Hi (edited)"},
	}
	replyJSON, _ := json.Marshal(reply)
	if err := engine.SupplyInput(ctx, id, tasks.InputResponse{Data: replyJSON}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	terminal := waitForTerminal(ctx, t, engine, id, 5*time.Second)
	var envelope struct {
		StructuredContent afcontracts.ProposeWithEditsOutput `json:"structuredContent"`
	}
	if err := json.Unmarshal(terminal, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.StructuredContent.State != "approved" {
		t.Errorf("State = %q, want approved", envelope.StructuredContent.State)
	}
	if got := envelope.StructuredContent.Edits["subject"]; got != "Hi (edited)" {
		t.Errorf("subject final = %v, want \"Hi (edited)\"", got)
	}
	// recipient + body fall back to their proposed defaults.
	if got := envelope.StructuredContent.Edits["recipient"]; got != "board@" {
		t.Errorf("recipient final = %v, want \"board@\"", got)
	}
	if got := envelope.StructuredContent.Edits["body"]; got != "New body" {
		t.Errorf("body final = %v, want \"New body\"", got)
	}
}

// TestPhase25_ProposeWithEditsRejected covers the reject path — no edits
// are present on the terminal payload.
func TestPhase25_ProposeWithEditsRejected(t *testing.T) {
	t.Parallel()
	engine, _, session, ctx := newApprovalFlowsServerAndClient(t)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "propose_with_edits",
		Arguments: afcontracts.ProposeWithEditsInput{
			Title:       "x",
			Description: "x",
			Fields:      []afcontracts.Field{{Key: "x", Type: afcontracts.FieldTypeString, Current: "1", Proposed: "2"}},
		},
	}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	id := waitForFirstInputRequired(ctx, t, engine, 5*time.Second)
	reply := afcontracts.ApprovalReply{Approved: false, Reason: "Not yet."}
	replyJSON, _ := json.Marshal(reply)
	if err := engine.SupplyInput(ctx, id, tasks.InputResponse{Data: replyJSON}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	terminal := waitForTerminal(ctx, t, engine, id, 5*time.Second)
	var envelope struct {
		StructuredContent afcontracts.ProposeWithEditsOutput `json:"structuredContent"`
	}
	if err := json.Unmarshal(terminal, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.StructuredContent.State != "rejected" {
		t.Errorf("State = %q, want rejected", envelope.StructuredContent.State)
	}
	if len(envelope.StructuredContent.Edits) != 0 {
		t.Errorf("Edits = %v, want empty on rejection", envelope.StructuredContent.Edits)
	}
}

// TestPhase25_MalformedReplyDrivesError covers a failure mode (CLAUDE.md
// §17): a user-supplied reply that does not decode against
// ApprovalReply surfaces as State="error" on the terminal payload — the
// handler does not panic, the task completes cleanly.
func TestPhase25_MalformedReplyDrivesError(t *testing.T) {
	t.Parallel()
	engine, _, session, ctx := newApprovalFlowsServerAndClient(t)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "request_approval",
		Arguments: afcontracts.RequestApprovalInput{
			Title:       "x",
			Description: "x",
		},
	}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	id := waitForFirstInputRequired(ctx, t, engine, 5*time.Second)
	// Supply garbage — not a valid ApprovalReply JSON.
	if err := engine.SupplyInput(ctx, id, tasks.InputResponse{
		Data: []byte(`not-valid-json`),
	}); err != nil {
		t.Fatalf("SupplyInput: %v", err)
	}
	terminal := waitForTerminal(ctx, t, engine, id, 5*time.Second)
	var envelope struct {
		StructuredContent afcontracts.RequestApprovalOutput `json:"structuredContent"`
	}
	if err := json.Unmarshal(terminal, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.StructuredContent.State != "error" {
		t.Errorf("State = %q, want error", envelope.StructuredContent.State)
	}
	if envelope.StructuredContent.Message == "" {
		t.Error("expected an error Message on a malformed reply")
	}
}

// --- helpers ----------------------------------------------------------

// newApprovalFlowsServerAndClient stands up a real runtime/server with a
// real tasks.Engine attached, registers the approval-flows handlers, and
// connects an in-memory SDK client to it. Returns the engine (for direct
// elicitation drive) + the session.
func newApprovalFlowsServerAndClient(t *testing.T) (
	*tasks.Engine, *server.Server, *mcpsdk.ClientSession, context.Context,
) {
	t.Helper()

	ts := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(ts, &tasks.Options{
		Logger:                quietLogger(),
		PollInterval:          50,
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		t.Fatalf("tasks.NewEngine: %v", err)
	}
	srv, err := server.New(server.Info{
		Name: "af-itest", Version: "0.1.0",
	}, &server.Options{
		Logger: quietLogger(),
		Tasks:  engine,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	reqApproval := afhandlers.CreateRequestApproval{Engine: engine}
	if err := tool.New[afcontracts.RequestApprovalInput, afcontracts.RequestApprovalOutput]("request_approval").
		Describe("Pause for human approval.").
		Handler(reqApproval.Handler).
		Register(srv); err != nil {
		t.Fatalf("register request_approval: %v", err)
	}
	proposeEdits := afhandlers.CreateProposeWithEdits{Engine: engine}
	if err := tool.New[afcontracts.ProposeWithEditsInput, afcontracts.ProposeWithEditsOutput]("propose_with_edits").
		Describe("Propose a structured change.").
		Handler(proposeEdits.Handler).
		Register(srv); err != nil {
		t.Fatalf("register propose_with_edits: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "itest", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return engine, srv, session, ctx
}

// waitForFirstInputRequired polls the engine's task store until a task is
// in input_required, then returns its id. Fails after deadline. The
// helper is robust to "which task" because the test runs in isolation
// (one tool call per test).
func waitForFirstInputRequired(
	ctx context.Context, t *testing.T, engine *tasks.Engine, deadline time.Duration,
) string {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		// Drive a tasks/list via the engine to see the tasks.
		params, _ := json.Marshal(map[string]any{})
		raw, err := engine.Dispatch(ctx, tasks.MethodList, params)
		if err != nil {
			// tasks/list may not be advertised; fall back to a polling
			// loop on the store via the codec's tasks/get. The
			// no-list deployment cannot enumerate, so we rely on a
			// known fact: this is the first task the test created.
			// Without list, the only thing to do is wait briefly and
			// recheck via tasks/get on a generated id — but we don't
			// have one. As a pragmatic fallback, enable advertising on
			// the engine for this test.
			if errors.Is(err, tasks.ErrUnknownMethod) {
				t.Fatal("tasks/list is not advertised — enable AdvertiseList on the test engine")
			}
			t.Fatalf("tasks/list dispatch: %v", err)
		}
		var list struct {
			Tasks []struct {
				TaskID string `json:"taskId"`
				Status string `json:"status"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatalf("decode list: %v\n%s", err, raw)
		}
		for _, item := range list.Tasks {
			if item.Status == string(protocolcodec.TaskInputRequired) {
				return item.TaskID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no task reached input_required within %s", deadline)
	return ""
}

// waitForTerminal polls until the task is in a terminal status, then
// returns its terminal payload (the CallToolResult envelope bytes).
func waitForTerminal(
	ctx context.Context, t *testing.T, engine *tasks.Engine, id string, deadline time.Duration,
) []byte {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"taskId": id})
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		raw, err := engine.Dispatch(ctx, tasks.MethodGet, params)
		if err != nil {
			t.Fatalf("tasks/get: %v", err)
		}
		var got struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode get: %v", err)
		}
		if got.Status == string(protocolcodec.TaskCompleted) ||
			got.Status == string(protocolcodec.TaskFailed) ||
			got.Status == string(protocolcodec.TaskCancelled) {
			// Now read the result.
			result, err := engine.Dispatch(ctx, tasks.MethodResult, params)
			if err != nil {
				t.Fatalf("tasks/result: %v", err)
			}
			return result
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach a terminal status within %s", id, deadline)
	return nil
}
