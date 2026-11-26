# ADR-0019: WebSocket library — `github.com/coder/websocket`

**Status:** Accepted · **Date:** 2026-11-21

## Context
The dashboard (Phase 2) streams metrics over WebSocket, and the terminal (Phase 3) and the events channel need it too. The Go standard library has no WebSocket server. [ADR-0013](0013-dependency-policy.md) says: standard library first, a dependency only when the alternative is worse.

## Decision
Use **`github.com/coder/websocket`** (ISC licence, no transitive dependencies, maintained, context-aware API, same-origin check built in). It is added as the only new Go dependency of Phase 2.

Rules that go with it:
1. The `Origin` header must match the request host (or `server.allowedHosts`; localhost patterns only in demo mode). The library's check is never disabled.
2. Every WebSocket route sits behind the same middleware chain as REST (host guard, lockdown, session, RBAC). The response-writer wrapper implements `http.Hijacker`.
3. Bounded resources: 8 connections per user, 64 in total; a slow consumer loses frames (`{"dropped":n}`) instead of blocking collection; every write has a 5 s timeout; the session is re-validated every 30 s and a connection from a remote origin is closed when lockdown is switched on.

## Consequences
+ Small, auditable code (≈150 lines for the metrics stream) and a ready base for the terminal's binary frames.
− One more module to keep patched; `go mod tidy` must record it in `go.sum`.

## Alternatives considered
- **Hand-written RFC 6455 server:** no dependency, but framing, masking, fragmentation, ping/pong and close handshakes are easy to get subtly wrong, and the terminal needs all of them.
- **`gorilla/websocket`:** equivalent, but no `context` integration and a longer maintenance gap in the past.
- **`golang.org/x/net/websocket`:** documented as unmaintained for new code.
- **Server-Sent Events / polling:** one-directional; unsuitable for the terminal and weaker for back-pressure handling.
