# ADR-0002: Go for the backend and helper

**Status:** Accepted · **Date:** 2026-11-11

## Context
Need a long-running system daemon with WebSockets, PTY handling, process control, easy packaging, low memory, and strong concurrency.

## Decision
Go (≥ 1.22 required, CI on 1.24; relies on the 1.22 `net/http` pattern mux). `yubeosd` is pure Go (static binary, UI embedded with `go:embed`). `yubeos-priv` uses cgo only for libpam. Stdlib `net/http` (Go 1.22+ pattern mux), `log/slog`, `encoding/json`; minimal third-party set per ADR-0013.

## Consequences
Single-binary deployment, small footprint, simple cross-compilation, strong stdlib. Less dynamic than Python for rapid scripting; mitigated by provider mocks.

## Alternatives considered
Python/FastAPI (TrueNAS-like middleware): faster to prototype but heavier packaging, GIL, runtime deps. Rust: excellent safety, higher development cost for a CRUD/system-glue product.
