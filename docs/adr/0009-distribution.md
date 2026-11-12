# ADR-0009: Ship as a layer (.deb/apt) on stock Ubuntu/Debian first

**Status:** Accepted · **Date:** 2026-11-11

## Context
The requested product is an 'OS/harness based on Ubuntu/Debian'. Building and maintaining a distribution adds large security and update burden before the control plane proves itself.

## Decision
v1.0: `.deb` + apt repo + installer script on Ubuntu 24.04 (primary) and Debian 13. Post-1.0: appliance ISO via `live-build` reusing the same packages.

## Consequences
Faster to market, normal OS updates, distro-provided drivers. Less 'appliance' feel until the ISO exists.

## Alternatives considered
Custom ISO first: rejected for 1.0. Docker-only deployment: cannot manage host hardware/ZFS safely.
