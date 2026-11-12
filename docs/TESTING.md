# Testing Strategy

## 1. Pyramid

| Layer | Tooling | Scope | Runs |
|---|---|---|---|
| Unit (Go) | `go test -race`, table tests | parsers, ranking, path policy, manifest validation, RBAC, ring buffers | every PR |
| Golden / fixture | captured real outputs under `test/fixtures` | `zpool`, `zfs`, `smartctl`, `lsblk`, `nvidia-smi`, `/proc` files, Samba `testparm` | every PR |
| Contract | OpenAPI validation middleware in test mode | every response/request vs spec | every PR |
| Unit (UI) | Vitest + Testing Library | hooks, components, theme engine, formatters | every PR |
| Component visual | Storybook + Playwright screenshots (3 presets) | components & key pages in `--demo` | every PR (diff review) |
| E2E | Playwright against `--demo` | login, dashboard live data, file ops (mock FS), disk alias, add-on install (mock), theme change | every PR |
| Integration (real) | VM lab (QEMU) via `make vm-test` | priv helper, credential store, WebAuthn/TOTP login, remote-mode origin rules, ACME (Pebble), ZFS create/scrub/replace, Samba, terminals, fsworker, Docker add-ons | nightly + before release |
| Security | `govulncheck`, `gosec`, CodeQL, `npm audit`, ZAP baseline vs demo, hostile-input suites, fuzzing (`go test -fuzz`) on parsers/path/manifest | | PR (static) / nightly (dynamic) |
| Performance | `k6` (API), soak script (metrics/ws), Lighthouse CI | budgets from ARCHITECTURE §1 & UI-DESIGN §6 | nightly |
| Accessibility | axe-core in Playwright | each page × 2 themes | every PR |

## 2. What must always have tests

- **Privileged methods:** valid case, each validation failure, option-injection (`-x`, `--flag`), NUL byte, over-length, non-existent identifier, wrong role, missing step-up token.
- **Parsers:** golden files from ≥ 2 versions of each tool; unknown/extra fields must not crash (fuzz).
- **Path handling:** traversal, symlink escape, TOCTOU swap, Unicode normalisation, long names.
- **Reconcilers:** idempotency (apply twice = no-op), drift detection, partial failure recovery.
- **Migrations:** upgrade from every previous schema version fixture.
- **Ranking:** deterministic golden outputs for each window; cold-start path.

## 3. Deterministic data

Demo mode and tests use seeded PRNG (`--demo-seed`) and a fake clock interface (`clock.Clock`) injected everywhere time matters (sessions, schedulers, rollups, ranking).

## 4. Test data hygiene

Fixtures are sanitised (serial numbers, WWNs, hostnames, IPs, MACs replaced) by the capture tool; CI rejects fixtures matching real-looking secrets (`gitleaks`).

## 5. Quality gates

Coverage is a signal, not a goal: ≥ 80 % on `internal/{auth,rbac,privd,files,storage,addons,repopapers}`, ≥ 70 % overall; **no merge** with failing required checks, new `// TODO` without issue link, or new debt without a TECH-DEBT entry. Flaky test policy: quarantined within 24 h, fixed within a week or deleted.

## 6. Release checklist (excerpt)

Clean install + upgrade tests on Ubuntu 24.04 and Debian 13 · destructive-operation negative tests · 24 h soak (3 dashboards, 2 terminals, 1 upload loop) · SBOM + signatures verified · docs and screenshots refreshed · CHANGELOG complete.
