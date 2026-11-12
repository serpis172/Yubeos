# Contributing to Yubeos

Thanks for helping. The project is contract-first and phase-driven; read [ARCHITECTURE](docs/ARCHITECTURE.md) and [ROADMAP](docs/ROADMAP.md) before opening a PR.

## Workflow

1. Pick an issue labelled with the current roadmap phase (`phase:N`). Work outside the current phase needs a maintainer's OK.
2. Contracts first: if you change behaviour visible over the API, the DB, or a manifest, update `docs/API.md` / OpenAPI / `docs/DATA-MODEL.md` / `schemas/` **in the same PR** and before the implementation commit.
3. Branch from `main`: `feat/<scope>-<topic>`, `fix/<scope>-<topic>`, `docs/<topic>`.
4. [Conventional Commits](https://www.conventionalcommits.org/): `feat(storage): list pools via zpool scriptable output`.
5. Open a PR. CI must be green. Use the checklist in [CODE-REVIEW](docs/engineering/CODE-REVIEW.md).
6. Squash-merge. One approval required; two for anything touching `cmd/yubeos-priv` or `internal/priv`.

## Rules that are not negotiable

- No shell strings. Commands are `exec.Command(absPath, args...)` with validated arguments.
- No new privileged RPC method without: schema, allow-list entry, audit event, unit test with a hostile input case.
- No new dependency without an entry in [ADR-0013 dependency policy](docs/adr/0013-dependency-policy.md) criteria (stdlib first).
- Every provider has a mock; every feature works in `--demo` mode.
- Debt is allowed only if registered in [TECH-DEBT](docs/engineering/TECH-DEBT.md).

## Sign-off

Contributions are accepted under the project license with a DCO sign-off (`git commit -s`).
