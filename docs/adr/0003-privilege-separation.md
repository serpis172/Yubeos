# ADR-0003: Unprivileged daemon + root helper with typed RPC

**Status:** Accepted · **Date:** 2026-11-11

## Context
A NAS control plane needs root-level actions (ZFS, PAM, Samba, Docker, PTY as other users). A single root web service makes any web bug a full compromise.

## Decision
Two processes: `yubeosd` (user `yubeos`, network-facing) and `yubeos-priv` (root, no network). Communication over a Unix socket using JSON-RPC 2.0 with an allow-listed registry of typed methods, peer-credential checks, per-call audit, class-based timeouts, and HMAC step-up tickets for destructive operations. The helper also owns the credential store (ADR-0016). Details: SECURITY.md §4.

## Consequences
Strong blast-radius reduction; extra IPC code, packaging of two units, and a demo-mode fake. Debugging crosses a process boundary (request IDs propagate).

## Alternatives considered
Single root daemon: rejected. `sudo` rules for a service user: rejected, argument-level control is unmanageable and error-prone. Cockpit-style bridge per session: heavier.
