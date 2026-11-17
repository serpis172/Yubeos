# Yubeos — developer entry points. See docs/DEVELOPMENT.md.
SHELL := /bin/bash
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/serpis172/Yubeos/internal/version.Version=$(VERSION) -X github.com/serpis172/Yubeos/internal/version.Commit=$(COMMIT)
DEMO_ADDR ?= 127.0.0.1:8443

.PHONY: help deps gen web build test lint fmt demo dev dev-web check-schemas docs deb e2e vm-test clean

help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

deps: ## download Go modules, npm packages and the Python tools used by lint
	$(GO) mod tidy
	cd web && npm ci
	python3 -m pip install --user --break-system-packages -q jsonschema pyyaml || python3 -m pip install --user -q jsonschema pyyaml

gen: ## regenerate the TypeScript API client from api/openapi.yaml
	cd web && npm run gen

web: ## build the web UI into web/dist (embedded by the Go build)
	cd web && npm run build

build: web ## static binaries into dist/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ ./cmd/yubeosd ./cmd/yubeos-priv ./cmd/yubeosctl

test: ## Go (race) + web unit tests
	$(GO) test -race ./cmd/... ./internal/... ./web
	cd web && npm test

lint: check-schemas ## vet, gofmt, tsc, eslint, no-raw-colors
	@test -z "$$(gofmt -l cmd internal web)" || (gofmt -l cmd internal web; echo "gofmt needed"; exit 1)
	$(GO) vet ./cmd/... ./internal/... ./web
	cd web && npm run typecheck && npm run lint

check-schemas: ## validate JSON Schemas and the OpenAPI document
	python3 scripts/validate-schemas.py

fmt: ## format Go sources
	gofmt -w cmd internal web

demo: web ## everything mocked, NO auth, loopback only: https://$(DEMO_ADDR)
	$(GO) run ./cmd/yubeosd --demo --listen $(DEMO_ADDR)

dev: ## backend (demo, plain HTTP on 127.0.0.1:8443) + Vite HMR on :5173 — Ctrl-C stops both
	@trap 'kill 0' INT TERM EXIT; \
	$(GO) run ./cmd/yubeosd --demo --no-tls --listen 127.0.0.1:8443 & \
	cd web && npm run dev & \
	wait

dev-web: ## Vite dev server only (start `go run ./cmd/yubeosd --demo --no-tls --listen 127.0.0.1:8443` yourself)
	cd web && npm run dev

docs: ## link/anchor check of the Markdown docs (needs lychee: https://lychee.cli.rs)
	lychee --offline --no-progress --include-fragments "**/*.md"

e2e: build ## real binaries: helper as root, daemon unprivileged, real Linux users (needs root — use the VM lab)
	sudo -E bash test/e2e/auth-smoke.sh dist

deb: ## planned for Phase 9 (packaging)
	@echo "make deb: not implemented yet — packaging is Phase 9 (docs/ROADMAP.md)"; exit 1

vm-test: ## planned: VM integration lab (docs/DEVELOPMENT.md §4), arrives with the first privileged feature
	@echo "make vm-test: not implemented yet — see docs/DEVELOPMENT.md §4"; exit 1

clean:
	rm -rf dist web/dist/* && touch web/dist/.gitkeep
