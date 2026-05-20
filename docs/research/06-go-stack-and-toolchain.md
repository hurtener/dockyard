# Brief 06 — Go-2026 no-CGo stack & toolchain

**Date:** 2026-05-20
**Sources:** see §7. All URLs were reachable via WebSearch/WebFetch on 2026-05-20 unless explicitly marked as a search-summary-only result.
**Status:** Draft for RFC-001-Dockyard

## 1. Why this brief exists

The braindump pins three things and then defers a fourth. Settled: **Go backend, no CGo** (CGo-free single static binary, like Harbor); **Svelte** for example/template UIs; **server-side only**, building on `github.com/modelcontextprotocol/go-sdk`. Deferred: the user wrote *"Go 2026 coding standards (to be researched)"*. This brief resolves that phrase into something concrete and specifies the full generator/CLI toolchain Dockyard needs.

Dockyard is, structurally, two Go programs: (a) the **`dockyard` CLI/generator** that scaffolds projects, runs `dev`, regenerates contracts, and packages binaries; and (b) the **generated app server**, a small MCP server with an embedded Svelte UI. Both must compile to a single static CGo-free binary and cross-compile cleanly. The hardest engineering problem named in the braindump is keeping **Go contracts ↔ JSON Schema ↔ TypeScript types in sync** — that codegen pipeline is the spine of this brief.

## 2. Findings

### 2.1 "Go 2026 coding standards" decoded — Go 1.26

The current release is **Go 1.26**, released February 2026. Concretely, "2026 standards" means:

- **Toolchain & language version.** Target `go 1.26` in `go.mod`. Note Go 1.26's `go mod init` now writes a *lower* version (`go 1.(N-1).0`) by default to encourage broad compatibility; Dockyard should explicitly pin `go 1.26` in its own modules and in generated `go.mod` files rather than rely on the default.
- **Generics are mature.** Generics (since 1.18) are fully idiomatic in 2026. Go 1.26 lifted the last awkward restriction: generic types may now refer to themselves in their own type-parameter list (`type Adder[A Adder[A]] interface{ Add(A) A }`). Dockyard's contract builder (`app.Tool(...).Input[T]().Output[T]()` from the braindump) is a textbook generics use case and is fully supported.
- **Structured logging is `log/slog`.** `slog` (since 1.21) is the standard. Go 1.26 adds `slog.NewMultiHandler(h1, h2, ...)` which fans one log record out to multiple handlers. This is directly useful for Dockyard's observability story: one handler emits human-readable text to the dev console, another emits structured JSON for the "observability-as-a-protocol" console. **Standard:** no `log.Printf`, no third-party loggers (`logrus`/`zap`) in generated code — `slog` only, with a JSON handler in production and a text handler under `dockyard dev`.
- **Error conventions.** `errors.Is`/`errors.As`, `%w` wrapping, sentinel errors, and `errors.Join` are the baseline. Generated handlers should wrap with context (`fmt.Errorf("validate %s: %w", tool, err)`) and never panic across the MCP boundary.
- **`go fix` is now a "modernizer."** Go 1.26 completely rewrote `go fix` on the `go vet` analysis framework; it now ships ~two dozen *modernizers* that suggest safe rewrites to newer idioms, plus a source-level inliner driven by `//go:fix inline` directives. `go fix` is now part of a healthy CI lane alongside `go vet`.
- **Lint config = golangci-lint v2.** golangci-lint **v2** (GA since March 2025) changed config: a top-level `version: "2"`, and `enable-all`/`disable-all` replaced by `linters.default` (`standard` | `all` | `fast` | `none`). `golangci-lint migrate` converts v1 configs. **Standard for Dockyard:** `version: "2"`, `linters.default: standard`, plus an explicit curated enable list (`errcheck`, `govet`, `staticcheck`, `revive`, `gosec`, `sloglint`, `errorlint`, `copyloopvar`-era checks). Ship this `.golangci.yml` *in generated projects* so every Dockyard app inherits the same bar — this is the "quality through the toolchain" thesis from the braindump.
- **`encoding/json/v2` — do not adopt yet.** `encoding/json/v2` + `encoding/json/jsontext` remain **experimental**, gated behind `GOEXPERIMENT=jsonv2`, and are *not* covered by the Go 1 compatibility promise as of Go 1.26. They were not promoted to stable in 1.26. Dockyard must stay on stable `encoding/json` (v1) for V1. Worth tracking for a later minor — v2's `omitzero`, cleaner streaming, and stricter decoding would benefit contract validation — but it is a post-V1 item.
- **Runtime.** The Green Tea GC is on by default in 1.26 (10–40% less GC overhead); cgo call overhead dropped ~30% — irrelevant to Dockyard since we are CGo-free, but it confirms the ecosystem direction. No action needed; just build with the default toolchain.

### 2.2 Embedding the Svelte build — `embed.FS`

`//go:embed` (stable since Go 1.16) is the canonical answer and needs no third-party library. The pattern: a `web/embed.go` file with `//go:embed all:dist` populating an `embed.FS`, served via `http.FileServerFS` with an SPA fallback to `index.html` for unknown paths. For Dockyard the embedded assets are *also* served as MCP `ui://` resources, not just over HTTP — the same `embed.FS` backs both the inspector's HTTP preview and the MCP resource handler.

Key constraints found:

- The Svelte build must produce **static output**. For plain Svelte+Vite the default `vite build` output works. For SvelteKit, `@sveltejs/adapter-static` is required (prerendered, no Node server) — confirmed by the Liip and PocketBase write-ups.
- `//go:embed` cannot reach outside its own module directory and ignores files starting with `_`/`.` unless `all:` is prefixed. Generated layout must keep `web/dist` inside the Go module tree.
- Build ordering matters: `web/dist` must exist *before* `go build`. If it is missing the build fails. Solution: a committed `.gitkeep`/placeholder `index.html` plus a `go:generate` or Makefile step that runs the Vite build first. `dockyard build` orchestrates this explicitly.

### 2.3 JSON Schema from Go structs

The decisive 2026 development: **Google released `github.com/google/jsonschema-go/jsonschema`** (Jan 2026) — and the **official MCP `go-sdk` already depends on it**. It does four things in one package: build schemas via Go API, marshal/unmarshal schemas, validate JSON values against a schema, and **infer schemas from Go types** via a `For` function. Inference reads the `json` tag for property names, treats `omitzero`/`omitempty` as "optional," and reads a `jsonschema` tag for descriptions.

This is a near-perfect fit and removes a hard decision: Dockyard should **not** pick `invopop/jsonschema` or `swaggest/jsonschema-go` as a parallel mechanism. Because the MCP `go-sdk` is a settled dependency and it uses `google/jsonschema-go` internally for tool input/output schemas, using anything else would create a second, divergent schema dialect. **Use `google/jsonschema-go` as the single schema engine.** (`invopop/jsonschema` is mature and Draft 2020-12-capable and remains a fallback if a gap appears, but defaulting to it would fork the schema toolchain.)

### 2.4 Go → TypeScript codegen

`gzuidhof/tygo` is the strongest option: it generates TypeScript from Go **source** (AST parsing, not reflection), so it preserves doc comments, understands constants/enums and generic types, and respects `json` (and `yaml`) tags. Config is a `tygo.yaml` listing packages and custom type mappings (e.g. `time.Time → string`). Reflection-based generators (`typescriptify-golang-structs`) lose comments and constants — a real downside for the polished generated code the braindump demands.

However, tygo emits TS *directly from Go AST* — it does not consume JSON Schema. That forces a pipeline choice (see §3).

### 2.5 Go CLI framework

Three real contenders:

- **`spf13/cobra`** — the de facto standard (35k+ stars), used by `kubectl`, `gh`, `hugo`, Docker. Nested commands, generated help, shell completions, large ecosystem, pairs with `viper` for config. Heavier; some boilerplate.
- **`alecthomas/kong`** — struct-tag-driven; you declare the CLI as a Go struct and Kong binds args/flags to fields. Very little boilerplate, type-safe, good for medium CLIs.
- **`urfave/cli`** — simpler, composable, flag-focused; lighter than Cobra but a smaller ecosystem.

**Recommendation: Cobra.** Dockyard's CLI (`new`, `dev`, `test`, `build`, `install`, `validate`, `generate`, `publish`) is a multi-verb tool with subcommands and host-specific install variants — exactly Cobra's sweet spot. Shell completions and the familiarity (`gh`/`kubectl` muscle memory) matter for a DX-first product. Kong is the credible alternative if boilerplate becomes a complaint, but Cobra's ecosystem and completion story win for V1.

### 2.6 File-watching / hot-reload for `dockyard dev`

Two Go-native options: **`air-verse/air`** (config-file driven, popular, actively maintained into 2026) and **`bokwoon95/wgo`** (`wgo run` = `go run` that reruns on file change; silent, minimal, watches `.go` by default, `-file` flag adds other extensions, supports running multiple commands in parallel).

Dockyard should **not shell out to either** — it should embed its own watcher using `fsnotify` (the standard cross-platform Go file-watch library) inside `dockyard dev`. Reason: `dockyard dev` is a multi-process orchestrator (Go server + Vite dev server + inspector + schema/codegen watcher) and needs to choreograph them — restart only the Go process on `.go` changes, re-run codegen on contract changes, leave Vite HMR to handle `.svelte`. Vite already provides hot-reload for the UI; Go has no in-process hot reload, so the Go server is *restarted*, not patched. `wgo`'s "parallel commands" model is the conceptual reference; `fsnotify` + `os/exec` is the implementation. This keeps `dockyard dev` a single self-contained binary with no external dev-tool dependency.

### 2.7 Vite/Svelte build integration

Vite is the Svelte build tool in 2026. Integration facts:

- **`dev` mode:** Vite dev server runs on its own port with HMR; the Go server proxies UI requests to it (or the inspector iframe points at the Vite URL). Generated `vite.config.ts` sets `base: './'` so embedded assets resolve with relative paths.
- **`build` mode:** `vite build` → static `web/dist` → consumed by `//go:embed`.
- The braindump's `web/src/generated/contracts.ts` is a *Vite source input*, so codegen must run **before** `vite build` and **before/while** the Vite dev server is running.

### 2.8 The no-CGo constraint and persistence

CGo-free is satisfied automatically by the above (`embed`, `slog`, `fsnotify`, `cobra`, `tygo`, `google/jsonschema-go`, the MCP `go-sdk` are all pure Go). The one place CGo classically sneaks in is **SQLite**. If Dockyard needs local persistence for observability/inspector state (trace history, fixture runs, multi-server console state), the answer is **`modernc.org/sqlite`** — a complete CGo-free pure-Go port of SQLite3 (currently tracking SQLite 3.53.x), a standard `database/sql` driver, cross-compiles cleanly. `zombiezen.com/go/sqlite` (pure-Go, built on modernc) is a nicer non-`database/sql` API alternative. `ncruces/go-sqlite3` (WASM-based) is a third CGo-free route. **Caveat:** none of these may set `CGO_ENABLED=1`; CI must enforce `CGO_ENABLED=0` on every build. Open question whether persistence is even needed in V1 vs. in-memory + JSON files (Q-5).

## 3. Go-flavored shapes / API sketches

### 3.1 Codegen pipeline — single source of truth

**The source of truth is the Go contract struct.** This is forced by the braindump ("contract-first," "Go-first contracts," `Input[ShowRevenueInput]()`) and confirmed by the MCP `go-sdk` using `google/jsonschema-go`. JSON Schema and TypeScript are *both downstream artifacts*; neither is authored by hand.

The naive pipeline `Go → JSON Schema → TS` is attractive (one schema, two consumers) but has a seam: **`tygo` does not read JSON Schema** — it reads Go source. A JSON-Schema-driven TS step would need a separate `json-schema-to-typescript` (npm) tool, adding a Node dependency to codegen. Two viable designs:

**Design A — fan-out from Go (recommended).**

```text
                    ┌─ google/jsonschema-go .For() ──► contracts.schema.json  (MCP wire contract)
internal/contracts  │
  *.go  (SoT)  ──────┤
                    └─ tygo ───────────────────────────► web/src/generated/contracts.ts (UI types)
```

Go structs are the SoT. Schema and TS are generated *independently from Go*, each by a pure-Go tool. `dockyard validate` then **cross-checks** schema vs. TS for drift (compile-time TS check + a schema-shape assertion). No Node dependency in the codegen path.

**Design B — schema-as-hub.** `Go → JSON Schema → TS` via `json-schema-to-typescript`. Single schema, but pulls Node into `dockyard generate`. Rejected for V1: it makes the generator depend on a Node toolchain even for projects that haven't run `npm install` yet.

**Adopt Design A.** Both generators are pure Go, run in-process inside the `dockyard` binary, no shell-out:

```go
// dockyard generate (sketch)
schema, err := jsonschema.For[contracts.ShowRevenueOutput](nil) // google/jsonschema-go
// → write internal/contracts/generated.schema.json

cfg := &tygo.Config{Packages: []*tygo.PackageConfig{{
    Path:       "github.com/acme/revenue-dashboard/internal/contracts",
    OutputPath: "web/src/generated/contracts.ts",
    TypeMappings: map[string]string{"time.Time": "string"},
}}}
gen := tygo.New(cfg); err = gen.Generate()
```

`dockyard dev`'s schema watcher (fsnotify on `internal/contracts/*.go`) re-runs both generators on save — this is the braindump's "schema watcher active / TypeScript types generated" line.

### 3.2 Embed pattern (generated `web/embed.go`)

```go
package web

import "embed"

//go:embed all:dist
var Assets embed.FS // backs both the ui:// MCP resource and the HTTP preview
```

### 3.3 Recommended toolchain table

| Concern | Choice | Tradeoff / why |
|---|---|---|
| Go version | **1.26**, pinned in `go.mod` | current release; generics + slog mature |
| Logging | **`log/slog`** (+ `NewMultiHandler`) | stdlib; fans out to console + observability protocol |
| Lint | **golangci-lint v2**, `default: standard` + curated enable | shipped in generated projects = uniform bar |
| JSON Schema | **`google/jsonschema-go`** | same engine as MCP `go-sdk`; avoids a second dialect |
| Go → TS | **`gzuidhof/tygo`** | AST-based, keeps comments/enums/generics, pure Go |
| CLI | **`spf13/cobra`** | multi-verb, completions, ecosystem familiarity |
| File watch | **`fsnotify`** inside `dockyard dev` | embedded orchestrator, no external dev tool |
| UI build | **Vite** (+ `adapter-static` if SvelteKit) | static `dist` for `//go:embed` |
| Embed | **`//go:embed all:dist`** | stdlib; zero dependency |
| SQLite (if needed) | **`modernc.org/sqlite`** | pure-Go, CGo-free, `database/sql` driver |
| JSON | **stdlib `encoding/json` v1** | json/v2 still experimental in 1.26 — defer |

## 4. Sharp edges & risks

- **R1 — Schema/TS drift (Design A).** Generating schema and TS from Go independently means a bug in one generator silently desyncs them. Mitigation: `dockyard validate` must cross-verify (TS compiles against a fixture typed by the schema; structural assertion). This is non-optional — it is the braindump's "generated types out of date = build blocker."
- **R2 — `embed` build ordering.** `go build` fails if `web/dist` is absent. `dockyard build` must run Vite first; CI and a placeholder file must guard the case where someone runs raw `go build`.
- **R3 — `google/jsonschema-go` is young (Jan 2026).** New package; inference edge cases (nested generics, `interface{}`, unions, recursive types) may be rough. Risk is bounded because the MCP `go-sdk` depends on it — its bugs are the SDK's bugs and get fixed upstream. Pin a version; track releases.
- **R4 — `tygo` generic-type handling.** tygo supports Go 1.18 generics but Go 1.26 self-referential generics are new; verify tygo handles Dockyard's contract generics before relying on them in contract structs. Keep contract structs deliberately *simple* (the braindump wants "boring, readable" generated code anyway).
- **R5 — `encoding/json/v2` temptation.** Do not enable `GOEXPERIMENT=jsonv2` in V1; it breaks the Go 1 compatibility promise and the MCP `go-sdk` is built against v1 semantics.
- **R6 — SQLite cross-compile matrix.** `modernc.org/sqlite` supports a fixed OS/arch set; verify it covers Dockyard's target triples (darwin/arm64, linux/amd64, linux/arm64, windows/amd64) before committing to SQLite-backed observability state.
- **R7 — CGo creep.** Any future dependency may transitively pull CGo. CI must hard-fail builds unless `CGO_ENABLED=0` and assert single static binary output.

## 5. What Dockyard must adopt / build / avoid

**Adopt:** Go 1.26 pinned; `log/slog` with `NewMultiHandler`; golangci-lint v2 config *shipped inside generated projects*; `google/jsonschema-go` as the one schema engine; `gzuidhof/tygo` for Go→TS; `spf13/cobra` for the CLI; `//go:embed all:dist`; Vite (+ `adapter-static` for SvelteKit); `modernc.org/sqlite` *if and only if* persistence is required.

**Build:** the **Design A codegen pipeline** running in-process in the `dockyard` binary (Go struct = SoT → schema + TS, independently) plus a **`dockyard validate` drift check**; an **embedded `fsnotify`-based dev orchestrator** that restarts the Go server, re-runs codegen, and supervises the Vite dev server as one process tree; `dockyard build` that sequences `vite build` → `go build` with the correct embed ordering; a CGo-free CI lane (`CGO_ENABLED=0`, `go vet`, `go fix`, `golangci-lint v2`, cross-compile matrix).

**Avoid:** `encoding/json/v2` / `GOEXPERIMENT=jsonv2` in V1; reflection-based Go→TS generators (lose comments/enums); a second JSON Schema library alongside `google/jsonschema-go`; a Node-dependent codegen path (Design B / `json-schema-to-typescript`); shelling out to `air`/`wgo` instead of an embedded watcher; any CGo dependency, including CGo SQLite drivers (`mattn/go-sqlite3`).

## 6. Open questions

- **Q-1.** Design A vs. B for codegen: accept the independent-generation drift risk (Design A, no Node) or accept a Node dependency in `dockyard generate` for a single-schema hub (Design B)? Brief recommends A — RFC must ratify.
- **Q-2.** Does the Svelte template use plain Svelte+Vite or SvelteKit? SvelteKit needs `adapter-static`; plain Svelte+Vite is simpler to embed. Affects the generated `web/` scaffold.
- **Q-3.** CLI: lock Cobra, or prototype Kong to measure boilerplate before committing? Cobra recommended; decision should be explicit.
- **Q-4.** Is `tygo` sufficient long-term, or does Dockyard eventually need its own Go-AST→TS generator for full control over generated-code style (the braindump's "boring, readable, editable" mandate)? Adopt tygo for V1; flag own-generator as a possible later investment.
- **Q-5.** Does V1 need persistent storage for inspector/observability state at all? If yes → `modernc.org/sqlite`; if in-memory + JSON fixtures suffice, defer the SQLite dependency entirely.
- **Q-6.** Should the `google/jsonschema-go` version be pinned to exactly the version the MCP `go-sdk` uses (to guarantee one schema dialect), and how is that kept in lockstep as the SDK updates?
- **Q-7.** Track `encoding/json/v2`: define the trigger (e.g. promotion to stable in Go 1.27/1.28) for a planned migration of contract validation.

## 7. Sources

All reachable on 2026-05-20. Items marked *(search summary)* were read via WebSearch result summaries rather than full-page fetch.

- Go 1.26 release notes — https://go.dev/doc/go1.26 (fetched)
- Go 1.26 release blog — https://go.dev/blog/go1.26 *(search summary)*
- Google JSON Schema package for Go — https://opensource.googleblog.com/2026/01/a-json-schema-package-for-go.html (fetched); package: `github.com/google/jsonschema-go/jsonschema`
- `encoding/json/v2` experiment — https://go.dev/blog/jsonv2-exp ; https://pkg.go.dev/encoding/json/v2 *(search summary)*
- golangci-lint v2 announcement — https://ldez.github.io/blog/2025/03/23/golangci-lint-v2/ ; config docs — https://golangci-lint.run/docs/configuration/ *(search summary)*
- `gzuidhof/tygo` — https://github.com/gzuidhof/tygo ; https://pkg.go.dev/github.com/gzuidhof/tygo *(search summary)*
- `invopop/jsonschema` — https://github.com/invopop/jsonschema ; `swaggest/jsonschema-go` — https://github.com/swaggest/jsonschema-go *(search summary)*
- Go CLI comparison — https://github.com/gschauer/go-cli-comparison ; https://mt165.co.uk/blog/golang-cli-library/ *(search summary)*
- `air-verse/air` — https://github.com/air-verse/air ; `bokwoon95/wgo` — https://github.com/bokwoon95/wgo *(search summary)*
- `embed` package — https://pkg.go.dev/embed ; Embed SvelteKit into a Go binary (Liip) — https://www.liip.ch/en/blog/embed-sveltekit-into-a-go-binary ; go:embed static assets — https://oneuptime.com/blog/post/2026-01-25-bundle-static-assets-go-embed/view *(search summary)*
- `modernc.org/sqlite` — https://pkg.go.dev/modernc.org/sqlite ; `zombiezen.com/go/sqlite` — https://pkg.go.dev/zombiezen.com/go/sqlite *(search summary)*
