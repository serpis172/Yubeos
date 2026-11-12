# ADR-0015: Per-session file workers running as the user (pending spike S-2)

**Status:** Proposed · **Date:** 2026-11-11

## Context
File operations must obey real Linux permissions. Go cannot safely change credentials per request.

## Decision
A small static `yubeos-fsworker` is started by the helper with the user's credentials per active session and controlled over a socketpair; file descriptors are returned for bulk transfer; paths resolved with `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)`.

## Consequences
Kernel enforces permissions; extra process per active user; spawn latency on first access.

## Alternatives considered
Run `yubeosd` as root and emulate permissions: rejected. `setfsuid` per thread: not reliable in Go's scheduler.
