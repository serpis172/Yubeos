# ADR-0012: Provider interfaces with first-class demo mode

**Status:** Accepted · **Date:** 2026-11-11

## Context
Hardware-dependent features (ZFS, SMART, GPU, RAPL, PAM, Docker) are hard to develop and test on laptops/WSL2 and in CI.

## Decision
Every integration is an interface with real and deterministic mock implementations; `--demo` wires all mocks (and an in-process fake helper) with a seed. Screenshots, E2E tests and docs run on demo mode.

## Consequences
Fast UI development anywhere, reproducible tests; mocks must be kept faithful (fixtures captured from real systems, VM nightly verification).

## Alternatives considered
Test only on real hardware: slow, unreproducible.
