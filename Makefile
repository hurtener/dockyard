.PHONY: help build test vet lint preflight drift-audit check-mirror install-hooks clean dev generate web web-install

help:
	@echo "Dockyard — make targets"
	@echo "  build           Build the dockyard binary (CGo-free static)"
	@echo "  test            go test -race ./..."
	@echo "  vet             go vet ./..."
	@echo "  lint            golangci-lint run"
	@echo "  generate        Run code generation (skipped until codegen lands)"
	@echo "  web             Frontend gate: type-check + unit tests for web/"
	@echo "  web-install     Install web/ frontend dependencies"
	@echo "  preflight       Build + run smoke checks + drift-audit"
	@echo "  drift-audit     Verify design coherence (RFC, plans, briefs, mirror)"
	@echo "  check-mirror    Verify AGENTS.md == CLAUDE.md"
	@echo "  install-hooks   Install the pre-commit hook (one-time, per clone)"
	@echo "  clean           Remove build artifacts"

GO_SOURCES := $(shell find . -name '*.go' -not -path './vendor/*' 2>/dev/null | head -1)

build:
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

# Frontend gate — type-check + unit tests for every web/ project. Gated like the
# Go code: CI runs `make web`, the smoke script asserts it passes. No-ops
# gracefully where npm is absent or no web/ project has landed yet.
web:
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "skip web: npm not installed"; \
	elif [ -f web/bridge/package.json ]; then \
		echo "== web/bridge: type-check + unit tests =="; \
		( cd web/bridge && \
			if [ ! -d node_modules ]; then npm ci --no-audit --no-fund; fi && \
			npm run gate ); \
	else \
		echo "skip web: no web/ project landed yet"; \
	fi

web-install:
	@if ! command -v npm >/dev/null 2>&1; then \
		echo "skip web-install: npm not installed"; \
	elif [ -f web/bridge/package.json ]; then \
		( cd web/bridge && npm ci --no-audit --no-fund ); \
	else \
		echo "skip web-install: no web/ project landed yet"; \
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
	@rm -rf web/bridge/coverage web/bridge/dist
	@find . -name '*.test' -delete 2>/dev/null || true
	@find . -name 'coverage.out' -delete 2>/dev/null || true
