# ADR-0007: REST + WebSocket, OpenAPI as source of truth

**Status:** Accepted · **Date:** 2026-11-11

## Context
Need typed, versioned contracts for UI, CLI and automation, plus real-time streams.

## Decision
REST/JSON under `/api/v1` described by OpenAPI 3.1; WebSockets only for metrics, events and terminal; errors as RFC 9457; async operations as jobs. TS client generated; handlers validated against spec in tests.

## Consequences
Clear contracts, tooling and docs for free; spec upkeep is required. gRPC/GraphQL not used.

## Alternatives considered
GraphQL: flexible but heavier and poorer fit for streaming/binary. gRPC-web: browser tooling friction. tRPC: couples to TS backend.
