# ADR-0001: Single monorepo

**Status:** Accepted · **Date:** 2026-11-11

## Context
Backend, helper, web UI, schemas, packaging and docs evolve together; contract changes touch several of them at once.

## Decision
One repository (`yubeos`) with `cmd/`, `internal/`, `web/`, `api/`, `schemas/`, `packaging/`, `docs/`. Add-on catalogue lives in a separate repo (`yubeos-addons`) because it has an independent release cadence and signing.

## Consequences
Atomic cross-layer PRs, one CI, one version. Larger clone; CI must path-filter jobs.

## Alternatives considered
Polyrepo (backend / web / packaging): rejected, contract drift and coordination cost.
