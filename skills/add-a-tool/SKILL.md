---
name: add-a-tool
description: "Add a new contract-first tool to a Dockyard MCP server. Use when implementing a new server capability: define typed Go input/output structs, write a handler, register it on the server, regenerate contracts, and add an entry to dockyard.app.yaml. Covers both blank-server and template-based projects."
license: Apache-2.0
metadata:
  framework: dockyard
  surface: handler
  verbs: "generate validate"
---

# Add a contract-first tool to a Dockyard server

A Dockyard tool is **contract-first** (P1, RFC §6): the typed Go input and
output structs are the source of truth; the JSON Schema the host sees is
*generated*, never hand-written. Adding a tool is four steps:

1. Define the input + output contracts in `internal/contracts/`.
2. Write the typed handler in `internal/handlers/` (or alongside `main.go`
   on a blank scaffold).
3. Register the tool on the server in `registerTools` (or equivalent).
4. Declare the tool in `dockyard.app.yaml`.

Then regenerate and validate.

## 1. Define the contracts

Add the typed structs in your contracts package. Each field uses standard
`json:` tags; the codegen reads them as the schema's property names.

```go
// internal/contracts/contracts.go
package contracts

// SummariseInput is the typed input for the summarise tool.
type SummariseInput struct {
    // Text is the input prose to summarise. Required.
    Text string `json:"text"`
    // MaxWords caps the summary length. Optional; defaults to 60.
    MaxWords int `json:"maxWords,omitempty"`
}

// SummariseOutput is the typed, UI-facing output for the summarise tool.
type SummariseOutput struct {
    // Summary is the produced summary.
    Summary string `json:"summary"`
    // WordCount is the summary's word count, after truncation.
    WordCount int `json:"wordCount"`
}
```

Conventions that pay off later:

- Document every field with a leading `//` comment — the codegen lifts the
  comment into the JSON Schema's `description`.
- Use `json:"name,omitempty"` for optional fields. The codegen reads
  `omitempty` to mark the field optional in the schema.
- Keep contracts in `internal/contracts/` so they're not part of your
  public API.

## 2. Write the handler

A handler is a function over the typed contract pair. The first parameter
is a `context.Context` (cancellation, deadlines, observability); the
second is the decoded input. Return a `tool.Result[Out]` — the runtime
splits its `Text` (model-facing) from its `Structured` (UI-facing) payload
per RFC §6.3.

```go
// internal/handlers/handlers.go (template-style) or alongside main.go (blank).
package handlers

import (
    "context"
    "strings"

    "github.com/hurtener/dockyard/runtime/tool"
    "myorg/my-server/internal/contracts"
)

// Summarise is the summarise tool's handler.
func Summarise(_ context.Context, in contracts.SummariseInput) (tool.Result[contracts.SummariseOutput], error) {
    summary := compact(in.Text, in.MaxWords)
    return tool.Result[contracts.SummariseOutput]{
        Text: summary,
        Structured: contracts.SummariseOutput{
            Summary:   summary,
            WordCount: len(strings.Fields(summary)),
        },
    }, nil
}
```

Return a non-nil `error` for a transport-level failure (your DB is down);
return a `tool.Result[Out]` with a UI-state field for a domain "empty" /
"permission denied". The runtime never panics across the MCP boundary
(AGENTS.md §13) — your handler must not panic either.

## 3. Register the tool

Add one builder chain to `registerTools` (or the equivalent file the
scaffold produced):

```go
import "github.com/hurtener/dockyard/runtime/tool"

func registerTools(srv *server.Server) error {
    if err := tool.New[contracts.GreetInput, contracts.GreetOutput]("greet").
        Describe("Greet a person by name and return the assembled greeting.").
        Handler(greet).
        Register(srv); err != nil {
        return err
    }
    // NEW:
    if err := tool.New[contracts.SummariseInput, contracts.SummariseOutput]("summarise").
        Describe("Summarise a block of prose down to ~60 words.").
        Handler(handlers.Summarise).
        Register(srv); err != nil {
        return err
    }
    return nil
}
```

For a tool that drives a UI (an MCP App), add `.UI("<app-name>")` after
`.Describe(...)` — see the `attach-a-ui-resource` skill.

For a tool that runs as a long-running task, see the `approval-flows`
template (`task_support: required` in the manifest, and the handler
exercises the `TaskHandle` API).

## 4. Declare the tool in the manifest

Append an entry to `dockyard.app.yaml`'s `tools[]`:

```yaml
tools:
  - name: greet
    description: Greet a person by name and return the assembled greeting.
    input: internal/contracts.GreetInput
    output: internal/contracts.GreetOutput
    task_support: forbidden
  # NEW:
  - name: summarise
    description: Summarise a block of prose down to ~60 words.
    input: internal/contracts.SummariseInput
    output: internal/contracts.SummariseOutput
    task_support: forbidden
```

The `input` / `output` fields are Go type references; the format is
`<package-path>.<TypeName>`. `internal/contracts.SummariseInput` resolves
against the project module.

## 5. Regenerate and validate

```bash
dockyard generate    # produces JSON Schema + TypeScript from the Go structs
dockyard validate    # checks every quality gate, including stale-codegen drift
go test ./...        # contract test still passes
```

`dockyard generate` is idempotent — a rerun with no contract change
produces byte-identical output (RFC §6.2). `dockyard validate` exits
non-zero on a build-blocker (e.g. the generated schema is stale relative
to the Go contract — Design A's drift catcher, RFC §6.2 + D-113).

## What good looks like

- `dockyard validate` reports `0 blockers`.
- `go test ./...` passes.
- `dockyard test` (the full gate) reports the tool's contract category
  green.
- In `dockyard inspect`, the Tools panel lists the new tool and you can
  fire a call from the Operator-Invoke surface (D-131) with the
  schema-derived synthetic fixture.

## What to do next

- Drive the tool from a UI ⇒ `attach-a-ui-resource` skill.
- Exercise the contract drift catcher ⇒ `define-contracts` skill.
- Live-edit + auto-restart while iterating ⇒ `run-the-dev-loop` skill.
- Manually fire the tool against the live server ⇒
  `test-with-the-inspector` skill.
