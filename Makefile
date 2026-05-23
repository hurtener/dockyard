.PHONY: help build test vet lint preflight drift-audit check-mirror install-hooks clean dev generate web web-install coverage bench inspector-bundle

help:
	@echo "Dockyard — make targets"
	@echo "  build           Build the dockyard binary (CGo-free static; embeds the inspector SPA)"
	@echo "  inspector-bundle  Build web/inspector and stage it into internal/inspector/dist/"
	@echo "  test            go test -race ./..."
	@echo "  coverage        Per-package coverage profile + the mechanical band gate"
	@echo "  bench           Run the Go benchmarks (on demand — not a CI gate)"
	@echo "  vet             go vet ./..."
	@echo "  lint            golangci-lint run"
	@echo "  generate        Run code generation (skipped until codegen lands)"
	@echo "  web             Frontend gate: type-check + unit tests + coverage for web/"
	@echo "  web-install     Install web/ frontend dependencies"
	@echo "  preflight       Build + run smoke checks + drift-audit"
	@echo "  drift-audit     Verify design coherence (RFC, plans, briefs, mirror)"
	@echo "  check-mirror    Verify AGENTS.md == CLAUDE.md"
	@echo "  install-hooks   Install the pre-commit hook (one-time, per clone)"
	@echo "  clean           Remove build artifacts"

GO_SOURCES := $(shell find . -name '*.go' -not -path './vendor/*' 2>/dev/null | head -1)

# inspector-bundle — produce the production web/inspector SPA bundle and stage
# it into internal/inspector/dist/, which the `//go:embed all:dist` directive
# in internal/inspector/assets_embed.go points at. `make build` depends on
# this so a `bin/dockyard` produced by the canonical build always embeds the
# real Svelte SPA, not the in-Go placeholder page (remediation R4 B1).
#
# The staged tree is .gitignored apart from a .gitkeep anchor, so rebuilds
# never dirty the working tree. Skips gracefully when npm or web/inspector is
# absent — the Go build then embeds only the .gitkeep, and the inspector
# falls back to its in-Go placeholder page.
inspector-bundle:
	@if [ ! -f web/inspector/package.json ]; then \
		echo "skip inspector-bundle: web/inspector not landed"; \
	elif ! command -v npm >/dev/null 2>&1; then \
		echo "skip inspector-bundle: npm not installed"; \
	else \
		echo "== inspector-bundle: vite build + stage into internal/inspector/dist =="; \
		( cd web/inspector && \
			if [ ! -d node_modules ]; then npm ci --no-audit --no-fund; fi && \
			npm run build ) || exit 1; \
		mkdir -p internal/inspector/dist; \
		find internal/inspector/dist -mindepth 1 ! -name '.gitkeep' -exec rm -rf {} +; \
		cp -R web/inspector/dist/. internal/inspector/dist/; \
	fi

build: inspector-bundle
	@if [ -f cmd/dockyard/main.go ]; then \
		CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/dockyard ./cmd/dockyard; \
	else \
		echo "skip build: cmd/dockyard/main.go absent (CLI phase not landed yet)"; \
	fi

test:
	@if [ -n "$(GO_SOURCES)" ]; then \
		CGO_ENABLED=1 go test -race ./...; \
	else \
		echo "skip test: no Go sources yet"; \
	fi
# CGO_ENABLED=1 is required by the -race detector. This is test-only; the
# shipped binary is still CGo-free — `make build` pins CGO_ENABLED=0.

# coverage — the mechanical AGENTS.md §11 coverage gate (Phase 21.5). Produces a
# per-package coverage profile, then runs the coveragecheck tool, which compares
# each package against its band in internal/coveragecheck/coverage.json and
# exits non-zero on any shortfall. CI runs `make coverage`, so a coverage
# regression fails the build. CGO_ENABLED=1 keeps the run consistent with
# `make test`; -covermode=atomic is required when the race detector is on.
coverage:
	@if [ -n "$(GO_SOURCES)" ]; then \
		CGO_ENABLED=1 go test -race -covermode=atomic \
			-coverprofile=coverage.out ./... >/dev/null && \
		go run ./internal/coveragecheck/cmd/coveragecheck \
			-profile coverage.out \
			-config internal/coveragecheck/coverage.json; \
	else \
		echo "skip coverage: no Go sources yet"; \
	fi

# bench — the Go benchmarks for the hot reusable artifacts (the obs ring buffer,
# the protocolcodec codecs, the Store drivers). Run on demand for a baseline and
# regression-spotting; NOT a CI gate. -race is deliberately omitted: a benchmark
# needs real numbers. The default -benchtime is fine; pass BENCHTIME to shorten.
BENCHTIME ?= 1x
bench:
	@if [ -n "$(GO_SOURCES)" ]; then \
		go test -run '^$$' -bench . -benchmem -benchtime=$(BENCHTIME) ./...; \
	else \
		echo "skip bench: no Go sources yet"; \
	fi

vet:
	@if [ -n "$(GO_SOURCES)" ]; then \
		go vet ./...; \
	else \
		echo "skip vet: no Go sources yet"; \
	fi

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		if [ -n "$(GO_SOURCES)" ]; then golangci-lint run; \
		else echo "skip lint: no Go sources yet"; fi; \
	else \
		echo "golangci-lint not installed; skipping"; \
	fi

generate:
	@if [ -f cmd/dockyard/main.go ]; then \
		go generate ./...; \
	else \
		echo "skip generate: codegen not landed yet"; \
	fi

# Every web/ project gated by `make web` / installed by `make web-install`.
# Add a project's directory here when it lands; both targets loop over the set.
WEB_PROJECTS := web/bridge web/ui web/inspector

# Frontend gate — type-check + unit tests for every web/ project. Gated like the
# Go code: CI runs `make web`, the smoke script asserts it passes. No-ops
# gracefully where npm is absent or no web/ project has landed yet.
web:
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "skip web: npm not installed"; \
	else \
		any=0; \
		for p in $(WEB_PROJECTS); do \
			if [ -f "$$p/package.json" ]; then \
				any=1; \
				echo "== $$p: type-check + unit tests =="; \
				( cd "$$p" && \
					if [ ! -d node_modules ]; then npm ci --no-audit --no-fund; fi && \
					npm run gate ) || exit 1; \
			fi; \
		done; \
		[ "$$any" -eq 1 ] || echo "skip web: no web/ project landed yet"; \
	fi

web-install:
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "skip web-install: npm not installed"; \
	else \
		any=0; \
		for p in $(WEB_PROJECTS); do \
			if [ -f "$$p/package.json" ]; then \
				any=1; \
				echo "== $$p: npm ci =="; \
				( cd "$$p" && npm ci --no-audit --no-fund ) || exit 1; \
			fi; \
		done; \
		[ "$$any" -eq 1 ] || echo "skip web-install: no web/ project landed yet"; \
	fi

preflight:
	@bash scripts/preflight.sh

drift-audit:
	@bash scripts/drift-audit.sh

check-mirror:
	@diff -q AGENTS.md CLAUDE.md >/dev/null \
		&& echo "OK: AGENTS.md == CLAUDE.md" \
		|| (echo "DRIFT: AGENTS.md != CLAUDE.md"; exit 1)

install-hooks:
	@bash scripts/install-hooks.sh

clean:
	@rm -rf bin/ dist/ build/
	@rm -f coverage.out
	@for p in $(WEB_PROJECTS); do rm -rf "$$p/coverage" "$$p/dist"; done
	@# The staged inspector bundle is a `make build` artifact. Clean it back to
	@# the .gitkeep anchor so a fresh build re-stages from web/inspector/dist/.
	@if [ -d internal/inspector/dist ]; then \
		find internal/inspector/dist -mindepth 1 ! -name '.gitkeep' -exec rm -rf {} + ; \
	fi
	@find . -name '*.test' -delete 2>/dev/null || true
	@find . -name 'coverage.out' -delete 2>/dev/null || true
