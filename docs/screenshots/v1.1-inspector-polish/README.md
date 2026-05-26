# v1.1 Wave A — inspector polish — demo screenshots

Captured live via Playwright against `examples/prompts-demo` running over
the streamable-HTTP transport on `127.0.0.1:8080`, with the inspector
attached at `127.0.0.1:7180`.

## What's here

- `prompts-panel-list.png` — the Prompts rail tab listing all three
  prompts the `examples/prompts-demo` server registers
  (`code_review`, `explain_error`, `summarize_for_review`). The panel
  uses the shared `DataTable` from `@dockyard/ui`; the required /
  optional argument-name pattern is summarised in the Arguments column.
- `prompts-panel-form.png` — the Invoke form for `code_review` after
  picking the row. The form is keyed by `PromptArgument.Name`
  (D-152 — prompts carry flat string arguments, not a structured
  object), with the required `diff` marked and the optional `language`
  / `rubric` listed.
- `prompts-panel-invoked.png` — a full-page capture after pressing
  **Invoke prompts/get**. The inspector backend opened a short-lived
  MCP client session against `http://127.0.0.1:8080`, called
  `prompts/get`, closed the session, and rendered the resulting four
  messages (one system, three user) in the result region. The "2
  events" counter in the footer reflects the obs/v1 `prompt.get`
  start/end pair the runtime emitted.
- `dockyard-dev-help.txt` — the `dockyard dev --help` output, showing
  the new `--no-inspector` and `--inspector-addr` flags.

## How to reproduce

```bash
# In one terminal (start the example over HTTP):
DOCKYARD_TRANSPORT=http go run ./examples/prompts-demo/cmd/server

# In a second terminal (attach the inspector on a known port):
./bin/dockyard inspect \
  --url http://127.0.0.1:8080 \
  --dir examples/prompts-demo \
  --port 7180 \
  --no-open

# Open http://127.0.0.1:7180, pick the Prompts tab, click code_review,
# fill the diff field, press Invoke prompts/get.
```

The auto-attach path (Item 1) gets you the same Prompts panel via a
single command — `dockyard dev` brings up the supervised Go server,
the inspector, and prints the inspector URL to stdout.
