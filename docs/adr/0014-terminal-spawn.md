# ADR-0014: Terminal sessions as systemd transient units (pending spike S-3)

**Status:** Proposed · **Date:** 2026-11-11

## Context
A shell spawned as a child of the sandboxed helper would inherit restrictions (e.g. NoNewPrivileges) and break `sudo`; sessions should also be visible/manageable by the OS.

## Decision
Spawn shells through `systemd-run --pty --uid=<user> --property=PAMName=login` from the helper, pass the PTY master FD to `yubeosd` (SCM_RIGHTS). Fallback if the spike fails: `fork` + `Credential` + `setsid` + `TIOCSCTTY` from a *separate, unsandboxed* spawner process.

## Consequences
Clean cgroup/session semantics and working `sudo`; depends on systemd behaviour validated in spike S-3 (criteria in terminal spec).

## Alternatives considered
Always-fork from helper: sandbox inheritance problem. `ttyd`/Cockpit terminal: extra component and auth bridge.
