# Roadmap and Build Order

The project is built in **phases**. A phase is *done* only when its Definition of Done (DoD) is met, CI is green and docs are updated. Phases are ordered to avoid rework: **foundations → security spine → read-only features → write features → ecosystem → release.**

## Build rules (apply to every phase)

1. **Contract first** — OpenAPI + schema + DATA-MODEL change merges *before* implementation.
2. **Mock first** — every provider gets its mock; UI is built against `--demo`, then real providers are added.
3. **Read before write** — a module's read-only view ships before any mutating action.
4. **Vertical slice per PR** — migration → repo layer → API → UI → tests → docs.
5. **Risky spikes are time-boxed** and recorded as ADRs (S-1 … S-4).
6. **No phase starts until the previous DoD is met**, except documented parallel tracks (UI components ‖ backend).
7. Every phase ends with a tagged pre-release (`v0.N.0`) installable in the VM test lab.

## Dependency graph

```
P0 Foundations ─▶ P1 Core spine (auth, priv, shell, themes) ─┬▶ P2 Dashboard
                                                             ├▶ P3 Terminal   (needs S-3)
                                                             ├▶ P4 File Explorer
                                                             └▶ P5 Storage (5a→5e) ─▶ P6 Sharing
P1 ─▶ P7 Add-ons (needs P5 datasets for appdata) ─▶ P8 Repo-Papers (independent; can run ‖ P7)
P2..P8 ─▶ P9 Hardening & Release 1.0
```

## Phases

### P0 — Foundations · `v0.0.x` — **status: implemented, 2 items deferred (checklist below)**
**Goal:** a repo where every later PR is cheap and safe.
**Deliver:** repo skeleton per [ARCHITECTURE §4.1](ARCHITECTURE.md#41-repository-layout-adr-0001); LICENSE, issue/PR templates, CODEOWNERS, Dependabot; `Makefile` (`make dev|test|lint|build|demo|docs`); Go module, `web/` scaffold (Vite+TS strict); CI (lint, unit, build, `govulncheck`, `npm audit`, OpenAPI lint, schema validation, link check); `api/openapi.yaml` with `/healthz`, `/system/info`, error model; config loader + schema; slog; DB open + migration runner + `0001_init.sql`; ADRs 0001–0014 accepted.
**DoD:** `make demo` serves an empty shell at `https://localhost:8443`; CI green on a PR from a fork; docs site/README render.
**Risks:** over-scaffolding → keep to what P1 needs (YAGNI).

**Status checklist** (verified by running the commands)

| Item | State |
|---|---|
| Repo skeleton, LICENSE (Apache-2.0), `.gitignore`, `.editorconfig`, templates, CODEOWNERS, Dependabot | ✅ |
| Go module, `cmd/yubeosd`, config (defaults → JSON file → `YUBEOS_*` env → flags; strict + validated), slog, graceful shutdown | ✅ |
| HTTPS with auto-generated self-signed ECDSA P-256 cert (reuse, renewal < 30 d, `0600` key) | ✅ |
| API spine: request IDs, access log, panic recovery, security headers/CSP, Host guard (DNS rebinding), RFC 9457 problems, **default-deny `require()`** (no auth yet ⇒ 401 outside `--demo`) | ✅ |
| Endpoints `GET /healthz`, `GET /api/v1/system/info` (contract in `api/openapi.yaml`) | ✅ |
| `--demo` mode (fake admin, mock capabilities; providers/seed arrive in Phase 2) | ✅ |
| Web: Vite + React 19 + TS strict, TanStack Query, router, shell (sidebar from the sketches, top bar, placeholders), 3 theme presets via tokens, generated API client, no-raw-colors check, embedded with `go:embed` (95 KB gzip) | ✅ |
| Tests: Go `-race` (api, config, tls, problem, clock, main) · vitest (nav, shell) | ✅ |
| CI workflow (go, web, contracts, docs links) and `make` targets | ✅ (workflow not yet run on GitHub) |
| ADRs 0001–0015 | ✅ |
| SQLite open + migration runner + `0001_init.sql` | ⏳ deferred → TD-010 |
| YAML config decoding | ⏳ deferred → TD-009 |

### P1 — Core spine · `v0.1.0`
Split into three parallel-able tracks:
- **1a Privilege spine:** `yubeos-priv` + RPC framing + registry + audit + `pam.authenticate`; in-process fake for demo; hostile-input tests.
- **1b Auth & session:** login/logout/me, sessions, CSRF, rate limiting, RBAC middleware (default-deny), TOTP, API tokens, audit hash chain, TLS self-signed.
- **1c Shell & theme engine:** React shell (sidebar/top bar/routes/lazy modules), generated API client, token system, 4 presets, theme editor (Appearance), i18n en/it, jobs tray, `/ws/events`.
**DoD:** log in with a real Linux account in a VM; unprivileged daemon verified (`ps`, capabilities); theme switch live; axe scan clean; priv hostile-input suite green.
**Risks:** PAM+cgo packaging; mitigated by building `yubeos-priv` in CI on Ubuntu 24.04 and Debian 13 containers.

### P2 — Dashboard · `v0.2.0`
2a collectors + ring buffers + `/metrics/*` + `/ws/metrics` (CPU, RAM+ARC, net) · 2b GPU (NVIDIA) + RAPL via helper + disk IO · 2c UI widgets per sketch, heat grid, responsive · 2d (optional) draggable layout.
**DoD:** acceptance in [dashboard §6](modules/dashboard.md#6-acceptance-criteria-phase-2); soak test passes.

### P3 — Terminal · `v0.3.0`
**Spike S-3 first** (systemd-run vs fork+Credential) → ADR-0014 finalised → `pty.spawn` + FD passing → WS bridge + ring buffer → xterm UI with tabs.
**DoD:** [terminal §5](modules/terminal.md#5-acceptance-criteria-phase-3).

### P4 — File Explorer · `v0.4.0`
**Spike S-2:** fsworker model (SCM_RIGHTS, `openat2`) on both distros → list/stat → download/ZIP → upload (atomic) → mkdir/rename/delete → UI table with tree rows, upload queue.
**DoD:** [files §5](modules/files.md#5-acceptance-criteria-phase-4); two-user permission test.

### P5 — Storage · `v0.5.0` (largest phase, strictly sequential)
- **5a Disks (read-only):** inventory + by-id identity + SMART read + UI list/detail/hover. *Fixtures from ≥ 3 real machines.*
- **5b Metadata & health:** alias/tags, system tags, SMART tests (job), health verdicts, dashboard disk widgets linked.
- **5c Pools (read-only):** `zpool list/status` parsers (2.2 text + 2.3 JSON), datasets read-only, UI.
- **5d Pools (write):** plan → create → scrub → replace → export; destroy with step-up; VM tests with virtual disks.
- **5e Datasets & snapshots:** CRUD, quotas, snapshot policies, rollback.
**DoD:** [storage §7](modules/storage.md#7-acceptance-criteria) 5a/5d criteria; no destructive path reachable without plan + step-up (negative tests).

### P6 — Sharing · `v0.6.0`
Samba renderer + `testparm` validation + reload, NFS exports, share UI, service control, drift detection, SMB user mapping.
**DoD:** Windows client mounts/reads/writes per ACL; drift scenario handled; reboot persistence.

### P7 — Add-ons · `v0.7.0`
7a manifest schema + validator + local catalogue · 7b compose lifecycle jobs via helper (install/start/stop/update/rollback/remove) · 7c reverse proxy + sidebar entries · 7d signed official catalogue + permission review UI.
**DoD:** [addons §7](modules/addons.md#7-acceptance-criteria-phase-7).

### P8 — Repo-Papers · `v0.8.0` (may run in parallel with P7)
Sources + scheduler + cache → repo snapshots & ranking → papers ranking → topics → UI → Sources page.
**DoD:** [repo-papers §7](modules/repo-papers.md#7-acceptance-criteria-phase-8).

### P9 — Hardening & Release · `v1.0.0`
Packaging (`.deb`, apt repo, installer, upgrade/rollback tested), AppArmor, full security review ([CODE-REVIEW](engineering/CODE-REVIEW.md) pass over privileged code), pen-test checklist, load/soak tests, docs site, screenshots, demo video, issue triage, tech-debt burn-down ([TECH-DEBT](engineering/TECH-DEBT.md)), 1.0 changelog.
**DoD:** clean install + upgrade from every previous pre-release on Ubuntu 24.04 and Debian 13; all P1–P8 acceptance tests green in nightly VM CI; zero open critical/high security findings.

## After 1.0 (candidates, unscheduled)

AMD/Intel GPU, alerts & notification channels, mdadm/LVM/btrfs backends, ZFS replication, native encryption UI, subdomain add-on proxy, ACME wizard, VM/libvirt module, arm64, appliance ISO (`live-build`), optional LLM summaries for Repo-Papers behind an add-on boundary, generative backgrounds.

## Version policy

SemVer. `0.x` may break config/DB with migration notes; from `1.0` DB migrations are automatic and config is backward-compatible within a major. Release train: pre-release at each phase DoD; patch releases as needed; minor every ~8 weeks after 1.0.

## Spike register

| ID | Phase | Question | Pass criteria | Recorded in |
|---|---|---|---|---|
| S-1 | P1 | Can `yubeos-priv` authenticate via PAM (cgo, `libpam`) and ship as a reproducible `.deb` for Ubuntu 24.04 + Debian 13? | Login works for local + LDAP-less accounts; build reproducible in CI containers | ADR-0010 addendum |
| S-2 | P4 | Is the per-session `fsworker` (FD passing, `openat2`) fast and safe enough? | p95 list < 300 ms on 10 k entries; escape/TOCTOU tests pass | ADR-0015 (new) |
| S-3 | P3 | `systemd-run --pty` transient units vs `fork + Credential` for terminals | See [terminal §3](modules/terminal.md#3-process-model-adr-0014) | ADR-0014 |
| S-4 | P5c | Can `zpool status` text be parsed robustly across OpenZFS 2.2/2.3 outputs? | Golden tests from ≥ 10 captured outputs (healthy, degraded, resilvering, scrubbing, errors, spares, cache/log, removed disk) | ADR-0006 addendum |
