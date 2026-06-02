.PHONY: help build test vet lint preflight drift-audit check-mirror install-hooks clean dev generate web web-install coverage bench inspector-bundle inspector-bundle-check docs docs-install

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
	@echo "  docs            Build the published tech-docs site (Phase 29; VitePress under docs/site/)"
	@echo "  docs-install    Install the docs/site/ npm dependencies"
	@echo "  clean           Remove build artifacts"

GO_SOURCES := $(shell find . -name '*.go' -not -path './vendor/*' 2>/dev/null | head -1)

# inspector-bundle — produce the production web/inspector SPA bundle and stage
# it into internal/inspector/dist/, which the `//go:embed all:dist` directive
# in internal/inspector/assets_embed.go points at. `make build` depends on
# this so a `bin/dockyard` produced by the canonical build always embeds the
# real Svelte SPA, not the in-Go placeholder page (remediation R4 B1).
#
# The staged tree is COMMITTED (D-187) so `go install …@latest` and the
# cross-compiled release binaries — neither of which runs this target — ship
# the real inspector instead of the placeholder. `inspector-bundle-check`
# regenerates it in CI and fails on drift. Skips gracefully when npm or
# web/inspector is absent — the Go build then embeds whatever is committed.
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

# inspector-bundle-check — the committed-bundle freshness gate (D-187). It
# rebuilds the SPA and fails if the committed internal/inspector/dist/ tree
# differs from a fresh build — so the bundle a `go install` user gets can never
# drift from web/inspector source. CI runs this with the same pinned node +
# committed package-lock as the committer, so vite's content-hashed output is
# reproducible. Skips gracefully where npm / web/inspector is absent.
inspector-bundle-check: inspector-bundle
	@if [ ! -f web/inspector/package.json ] || ! command -v npm >/dev/null 2>&1; then \
		echo "skip inspector-bundle-check: web/inspector or npm absent"; \
	elif ! git diff --quiet -- internal/inspector/dist; then \
		echo "ERROR: committed internal/inspector/dist is stale — run 'make inspector-bundle' and commit the result:"; \
		git --no-pager diff --stat -- internal/inspector/dist; \
		exit 1; \
	else \
		echo "inspector-bundle-check: committed SPA bundle is fresh"; \
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
#
# The `go test` output is captured to a temporary log so a clean run stays
# quiet but a failing run surfaces the offending test name + assertion
# (Phase 27 — CI diagnostic hygiene). The pre-Phase-27 recipe redirected
# unconditionally to /dev/null, which left a CI flake undiagnosable from the
# run log alone — the same diagnostic hygiene fix the R4 `make web` pattern
# applied. The log file is removed on a successful run; on failure, it is
# preserved at coverage-test.log AND printed inline so a CI step's log
# surfaces the failure without requiring artifact upload.
coverage:
	@if [ -n "$(GO_SOURCES)" ]; then \
		log=$$(mktemp -t dockyard-coverage.XXXXXX) ; \
		if CGO_ENABLED=1 go test -race -covermode=atomic \
				-coverprofile=coverage.out ./... >"$$log" 2>&1 ; then \
			rm -f "$$log" ; \
			go run ./internal/coveragecheck/cmd/coveragecheck \
				-profile coverage.out \
				-config internal/coveragecheck/coverage.json ; \
		else \
			status=$$? ; \
			echo "FAIL: go test failed during coverage run (status $$status); log:" ; \
			cp "$$log" coverage-test.log ; \
			cat "$$log" ; \
			rm -f "$$log" ; \
			exit $$status ; \
		fi ; \
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

# docs — build Dockyard's published technical-documentation site (Phase 29,
# decision D-137: VitePress under docs/site/, deployed to GitHub Pages by
# .github/workflows/docs.yml). The target also regenerates the auto-derived
# CLI reference page (internal/clidocs) so a verb / flag rename is reflected
# in the published docs without a hand edit (AGENTS.md §19 hygiene).
#
# Skips gracefully where docs/site/package.json is absent (the docs surface
# has not landed yet) or npm is missing (CI may run a build-only path). The
# CLI-reference regen step runs unconditionally when docs/ exists because
# it is just `go run` against the in-tree CLI tree.
docs:
	@if [ ! -d docs/site ]; then \
		echo "skip docs: docs/site/ not present"; \
	elif [ ! -f docs/site/package.json ]; then \
		echo "skip docs: docs/site/package.json missing"; \
	elif ! command -v npm >/dev/null 2>&1; then \
		echo "skip docs: npm not installed"; \
	else \
		echo "== docs: regenerate CLI reference =="; \
		mkdir -p docs/site/cli; \
		go run ./internal/clidocs/cmd/clidocs -out docs/site/cli/index.md; \
		echo "== docs: build VitePress site =="; \
		( cd docs/site && \
			if [ ! -d node_modules ]; then npm ci --no-audit --no-fund; fi && \
			npm run build ) || exit 1; \
	fi

docs-install:
	@if [ ! -f docs/site/package.json ]; then \
		echo "skip docs-install: docs/site/package.json missing"; \
	elif ! command -v npm >/dev/null 2>&1; then \
		echo "skip docs-install: npm not installed"; \
	else \
		( cd docs/site && npm ci --no-audit --no-fund ) || exit 1; \
	fi

clean:
	@rm -rf bin/ dist/ build/
	@rm -f coverage.out
	@for p in $(WEB_PROJECTS); do rm -rf "$$p/coverage" "$$p/dist"; done
	@# Docs site build artefacts (Phase 29). node_modules is left intact
	@# so a rebuild reuses the install; remove it manually if needed.
	@rm -rf docs/site/.vitepress/dist docs/site/.vitepress/cache
	@# The staged inspector bundle is a `make build` artifact. Clean it back to
	@# the .gitkeep anchor so a fresh build re-stages from web/inspector/dist/.
	@if [ -d internal/inspector/dist ]; then \
		find internal/inspector/dist -mindepth 1 ! -name '.gitkeep' -exec rm -rf {} + ; \
	fi
	@find . -name '*.test' -delete 2>/dev/null || true
	@find . -name 'coverage.out' -delete 2>/dev/null || true
