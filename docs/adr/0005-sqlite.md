# ADR-0005: SQLite as the only database

**Status:** Accepted · **Date:** 2026-11-11

## Context
Single-node application; data volume is small (config, sessions, audit, rollups, Repo-Papers cache).

## Decision
SQLite in WAL mode via a pure-Go driver (no cgo in `yubeosd`), embedded forward-only migrations, nightly backups.

## Consequences
Zero operations, atomic backups, easy fixtures. Single writer; revisit if write rate or multi-node needs appear.

## Alternatives considered
PostgreSQL: unnecessary operational weight. BoltDB/files: weaker querying for audit/ranking.
