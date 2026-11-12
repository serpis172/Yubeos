# Yubeos — developer entry points. See docs/DEVELOPMENT.md.
SHELL := /bin/bash
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/OWNER/yubeos/internal/version.Version=$(VERSION) -X github.com/OWNER/yubeos/internal/version.Commit=$(COMMIT)
DEMO_PORT ?= 8443

.PHONY: help deps gen web build test lint fmt demo dev-web check-schemas clean

help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

deps: ## download Go modules and install npm packages
	$(GO) mod download
	cd web && npm ci

gen: ## regenerate the TypeScript API client from api/openapi.yaml
	cd web && npm run gen

web: ## build the web UI into web/dist (embedded by the Go build)
	cd web && npm run build

build: web ## static binaries into dist/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/yubeosd ./cmd/yubeosd

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

demo: web ## everything mocked, deterministic: https://localhost:$(DEMO_PORT)
	$(GO) run ./cmd/yubeosd --demo --listen :$(DEMO_PORT)

dev-web: ## Vite dev server with HMR (run `go run ./cmd/yubeosd --demo --no-tls` in another terminal)
	cd web && npm run dev

clean:
	rm -rf dist web/dist/* && touch web/dist/.gitkeep
