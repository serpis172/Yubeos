# Technical Debt Policy and Register

Debt is acceptable only when **deliberate, recorded and scheduled**. Accidental debt found in review becomes a register entry or a fix in the same PR.

## Categories

| Type | Examples |
|---|---|
| Code | duplication, leaky abstractions, magic numbers |
| Architecture | wrong boundaries, premature coupling between modules |
| Test | missing integration coverage, flaky tests |
| Dependency | outdated/unmaintained libs |
| Documentation | stale docs, missing runbooks |
| Infrastructure | manual release steps, missing monitoring |
| Security | accepted residual risks, deferred hardening |

## Scoring

```
Priority = (Impact + Risk) × (6 − Effort)         each 1–5; higher priority = do sooner
```

## Rules

1. Every `// TODO`/`// ponytail:` simplification that cuts a known corner has a register ID and a **ceiling** (the condition under which it must be revisited).
2. Max 10 open items with priority ≥ 40; above that, feature work pauses until under the limit.
3. Each phase ends with a **debt review**: re-score, close or re-plan; ≥ 20 % of P9 capacity is reserved for burn-down.
4. Security-category items are reviewed at every release.
5. Remediation is done alongside feature work in small PRs (`chore(debt): TD-012 …`).

## Register

| ID | Category | Item | Impact | Risk | Effort | Priority | Revisit when / plan |
|---|---|---|:-:|:-:|:-:|:-:|---|
| TD-001 | Security | CSP `style-src 'unsafe-inline'` required by runtime theme tokens | 2 | 3 | 3 | 15 | Move to CSSOM `adoptedStyleSheets`/nonce; before 1.0 |
| TD-002 | Architecture | In-memory 1 h metrics ring; short history lost on restart | 2 | 1 | 2 | 12 | Users request persistent 1 s history |
| TD-003 | Architecture | `nvidia-smi` stream instead of NVML | 2 | 2 | 4 | 8 | Need MIG/sub-second data |
| TD-004 | Security | No custom CSS in themes | 1 | 1 | 3 | 6 | Demand; admin-only + sanitiser |
| TD-005 | Infrastructure | Self-signed TLS default | 3 | 3 | 2 | 24 | ACME wizard post-1.0; installer prints fingerprint |
| TD-006 | Architecture | Add-on UIs only via sub-path proxy or direct link | 3 | 2 | 4 | 10 | Subdomain proxy (Phase 7b) |
| TD-007 | Test | Integration tests need VM (not on GitHub-hosted runners: no ZFS module) | 3 | 3 | 3 | 18 | Self-hosted runner or nightly Lima/QEMU job |
| TD-009 | Dependency | Config loader reads JSON only; `yaml.v3` could not be fetched in the build sandbox (Go proxy blocked) | 2 | 2 | 1 | 20 | Phase 1: add `yaml.v3` decoder behind the loader; existing JSON files stay valid |
| TD-010 | Architecture | SQLite layer (`internal/db`, migrations) not built: `modernc.org/sqlite` could not be fetched in the sandbox | 4 | 3 | 2 | 28 | **First task of Phase 1b** (sessions/audit need it): `go get modernc.org/sqlite` on a networked machine |
| TD-011 | Test | Frontend has no visual-regression/E2E tests yet (Storybook + Playwright planned) | 2 | 2 | 3 | 12 | Phase 1c, when components stabilise |
| TD-008 | Code | Repo ranking star-growth depends on own snapshots (no public history API) | 3 | 2 | 4 | 10 | Optional aggregate-data bootstrap |
