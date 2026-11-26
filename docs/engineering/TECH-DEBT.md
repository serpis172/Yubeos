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
| TD-006 | Architecture | Isolated-subdomain add-on proxy needs wildcard cert (DNS-01 ACME planned v1.1); LAN users get the direct-link fallback | 3 | 2 | 4 | 10 | Phase 7c implements ADR-0018; DNS-01 in v1.1 |
| TD-007 | Test | Integration tests need VM (not on GitHub-hosted runners: no ZFS module) | 3 | 3 | 3 | 18 | Self-hosted runner or nightly Lima/QEMU job |
| ~~TD-009~~ | Dependency | ~~Config loader JSON only~~ **Closed 2026-11-14:** YAML + JSON, same strictness | — | — | — | — | done |
| ~~TD-010~~ | Architecture | ~~SQLite layer missing~~ **Closed 2026-11-14:** `internal/db` + migrations (pure-Go driver chosen in `driver.go`; verified here against the C driver, see TD-020) | — | — | — | — | done |
| TD-011 | Test | Frontend has no visual-regression/E2E tests yet (Storybook + Playwright planned) | 2 | 2 | 3 | 12 | Phase 1c, when components stabilise |
| TD-008 | Code | Repo ranking star-growth depends on own snapshots (no public history API) | 3 | 2 | 4 | 10 | Optional aggregate-data bootstrap |
| TD-012 | Infrastructure | GitHub Actions pinned by tag, not SHA (could not resolve SHAs from the build sandbox) | 3 | 2 | 1 | 25 | Run `scripts/pin-actions.sh` once with network; Dependabot keeps SHAs fresh |
| TD-013 | Architecture | Built-in ACME not implemented: `tls.mode=acme` is rejected by config validation | 3 | 3 | 3 | 18 | Phase 1d (spike S-5) |
| TD-014 | Security | `yubeos-priv.service` has no `SystemCallFilter` yet | 3 | 2 | 3 | 15 | Validate with `systemd-run` terminals (S-3), then enable in P9 hardening |
| TD-015 | Dependency | `go.mod` declares `go 1.26.0`; CI now reads the version from `go.mod` (it used 1.24 and only worked through toolchain auto-download). README states the same minimum | 2 | 2 | 1 | 20 | Raise to the then-current stable on the first networked build; must keep `go vet`/tests green |
| TD-016 | UX | No i18n yet (en/it planned Phase 1e), no login UI, no command palette | 3 | 1 | 3 | 12 | Phase 1e |
| TD-017 | Security | Password denylist is a ~50-entry starter list, not the 10 000 most common passwords | 2 | 2 | 2 | 16 | Embed a vetted list (licence-checked) in Phase 1c |
| TD-018 | Security | Global login cool-down (≥ 100 failures / 10 min across all IPs) not implemented | 3 | 2 | 2 | 20 | Phase 1d |
| TD-019 | Security | Password change uses the current password as proof instead of a second-factor step-up | 3 | 2 | 2 | 20 | Phase 1c (step-up tickets) |
| TD-020 | Test | `modernc.org/sqlite` (the shipped driver) could not be fetched in the sandbox; tests there ran on the same SQL through the C driver `mattn/go-sqlite3`. SQL used is standard (WAL, FKs, `VACUUM INTO`) but CI must confirm on the pure-Go driver | 3 | 2 | 1 | 25 | First CI run after `make deps`; fix any dialect difference |
| TD-021 | Feature | API tokens (table exists) and `auth.mode: pam` are not implemented | 2 | 1 | 3 | 9 | Phase 1c (tokens); PAM post-1.0 or dropped |
| TD-022 | Test | `yubeos-priv` and `yubeosd` units are not validated with `systemd-analyze verify/security` (no systemd in the sandbox) | 3 | 3 | 1 | 24 | Run in the VM lab; adjust capabilities |
| TD-023 | Security | Epoch check fails open when the helper is unreachable (availability over strictness, logged) | 2 | 2 | 2 | 16 | Reconsider with remote mode (fail closed for remote origins) |
