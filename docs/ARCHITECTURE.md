# Architecture

Status: **Accepted (v0.1 spec)** · Related: [SECURITY](SECURITY.md), [API](API.md), [DATA-MODEL](DATA-MODEL.md), [ADRs](adr/README.md)

## 1. Context and goals

Yubeos is a web control plane for a single Linux home server. Operators manage hardware telemetry, storage, files, a shell, containerised apps and UI appearance from one browser tab.

**Functional goals** — the seven sections in the [README](../README.md): Dashboard, File Explorer, Disk Manager, Terminal, Add-ons, Repo-Papers, Appearance.

**Non-functional targets (v1.0)**

| Area | Target |
|---|---|
| Idle footprint | `yubeosd` RSS ≤ 80 MB; `yubeos-priv` ≤ 20 MB; CPU < 1 % on 8 cores with one dashboard open |
| Live latency | Metric sample → pixels ≤ 1.5 s on LAN |
| UI load | First contentful paint ≤ 2 s on LAN; shell bundle ≤ 400 KB gzip, modules lazy-loaded |
| Concurrency | 20 sessions, 10 terminals, 5 concurrent uploads without degrading metrics |
| Availability | Daemon crash ⇒ systemd restart ≤ 5 s; **no data-path dependency** (Samba, ZFS, Docker keep serving without Yubeos) |
| Security | No root in the network-facing process; every state-changing call audited |
| Portability | Ubuntu 24.04 LTS, Debian 13, x86_64; whole UI runs in `--demo` on any Linux/WSL2 |

**Non-goals (v1.0):** custom kernel or installer ISO · hypervisor/VM management · clustering/HA · replacing Samba/ZFS/Docker · LLM/AI features · mobile app · non-Linux hosts.

## 2. Principles

1. **Linux is the source of truth for what exists; Yubeos' database is the source of truth for intent and metadata.** Pools, datasets and containers are read from the system. Aliases, tags, share definitions, themes and add-on desired state live in SQLite.
2. **Reconcile, don't script.** Managed config (e.g. Samba shares) is *rendered* from DB state into a Yubeos-owned include file and applied idempotently; Yubeos never edits files it does not own.
3. **Typed privileged operations.** The root helper exposes named RPC methods, never "run this command".
4. **Providers behind interfaces.** Every hardware/OS integration is an interface with a real and a mock implementation.
5. **Contract first.** OpenAPI + JSON Schema precede implementation; the TypeScript client is generated.
6. **Vertical slices.** A feature ships as schema → mock provider → API → UI → real provider → tests → docs.

## 3. System context

```
              ┌───────────────────────────── Browser ──────────────────────────────┐
              │ React SPA (served by yubeosd)                                       │
              │ Dashboard · Files · Disks · Terminal · Add-ons · Repo-Papers · …    │
              └──────────────┬──────────────────────────────┬──────────────────────┘
                 HTTPS REST  │ (JSON, problem+json)         │ WebSocket (metrics, events, terminal)
┌────────────────────────────▼──────────────────────────────▼────────────────────────┐
│ yubeosd  (user: yubeos · unprivileged · network-facing · systemd-hardened)          │
│  http · auth/session · rbac · audit · jobs · event bus · collectors · cache         │
│  modules: dashboard │ files │ storage │ terminal │ addons │ repopapers │ appearance │
│  SQLite (/var/lib/yubeos/yubeos.db)                                                 │
└────────────────┬────────────────────────────────────────────────────────────────────┘
                 │ Unix socket /run/yubeos/priv.sock  (JSON-RPC 2.0, SO_PEERCRED-checked)
┌────────────────▼────────────────────────────────────────────────────────────────────┐
│ yubeos-priv  (root · no network · allow-listed typed RPC · audits every call)       │
│  auth store · zfs · smart · rapl · samba/nfs · docker compose · pty/fs workers        │
└───────┬────────┬─────────┬──────────┬──────────┬──────────┬─────────────────────────┘
        ▼        ▼         ▼          ▼          ▼          ▼
    cred.db   OpenZFS   smartctl   Samba/NFS   Docker   systemd (transient units)
```

Network egress (yubeosd only): GitHub, arXiv, Semantic Scholar, Hugging Face (Repo-Papers); add-on catalogue over git/HTTPS. All optional and individually disableable.

## 4. Components

| Component | Language | Runs as | Responsibility |
|---|---|---|---|
| `yubeosd` | Go | `yubeos` | HTTP/WS API, embedded UI (`go:embed`), auth, RBAC, audit, jobs, collectors, module logic, SQLite |
| `yubeos-priv` | Go (cgo only with optional `pam` build tag) | `root` | Only component allowed to touch privileged resources **and the credential store** — see [SECURITY](SECURITY.md), [AUTHENTICATION](AUTHENTICATION.md) |
| `yubeosctl` | Go | any | Local CLI: `status`, `doctor`, `reset-2fa`, `export-config`, `demo` |
| `web/` | TypeScript/React | browser | SPA, generated API client, theme engine |
| `packaging/` | shell, systemd, nfpm | — | `.deb`, unit files, installer, AppArmor profile |
| `addons/` | YAML | — | First-party add-on manifests; community catalogues use the same schema |

### 4.1 Repository layout ([ADR-0001](adr/0001-monorepo.md))

```
yubeos/
├── cmd/{yubeosd,yubeos-priv,yubeosctl}/
├── internal/
│   ├── api/          routing, middleware, problem+json, OpenAPI handlers
│   ├── auth/         sessions, TOTP, API tokens, CSRF, rate limit
│   ├── rbac/  audit/  config/  events/  jobs/
│   ├── db/           SQLite open, embedded .sql migrations, repositories
│   ├── metrics/      cpu/ mem/ net/ gpu/ disk/  (each: provider.go linux.go mock.go)
│   ├── storage/      Backend interface; zfs/ plain/ mock/
│   ├── shares/       SMB/NFS model + renderers
│   ├── files/        path policy, listing, transfer
│   ├── terminal/     session registry, ring buffer, WS bridge
│   ├── addons/       manifest, catalogue, lifecycle, reverse proxy
│   ├── repopapers/   sources/ ranking/ scheduler/
│   ├── appearance/   themes, preferences, fonts
│   ├── priv/         RPC client (yubeosd side) + shared wire types
│   └── privd/        RPC server + method handlers (yubeos-priv side)
├── web/              Vite + React + TS (src/modules/<section>/)
├── api/openapi.yaml  HTTP contract — source of truth
├── schemas/          JSON Schemas: theme, addon manifest, config
├── addons/           first-party manifests
├── packaging/        deb, systemd, installer, apparmor
├── test/             integration + VM tests, fixtures (zpool/smartctl/nvidia-smi outputs)
├── docs/
└── .github/          workflows, templates, CODEOWNERS, dependabot
```

Go module: `github.com/serpis172/Yubeos`. Everything under `internal/` is private to the repo.

## 5. Process model and trust boundaries

| Boundary | Mechanism | Rule |
|---|---|---|
| Browser → `yubeosd` | HTTPS, session cookie, CSRF header, Origin check on WS | Authenticated except `/api/v1/auth/login`, `/healthz`, static assets |
| `yubeosd` → `yubeos-priv` | Unix socket + peer credentials | Only uid `yubeos`; method allow-list; typed params; per-call audit |
| `yubeos-priv` → OS | `exec` with absolute paths + arg arrays, or direct syscalls | No shell, scrubbed env, timeouts, output caps |
| `yubeosd` → Internet | HTTPS client, per-module host allow-list | Timeouts, size caps, no redirect off allow-list |
| Add-on containers → host | Docker defaults + manifest-declared permissions | Privileged / docker.sock mounts need explicit admin approval |

Threat model: [SECURITY](SECURITY.md).

## 6. Provider pattern and demo mode ([ADR-0012](adr/0012-demo-mode.md))

Each integration is a Go interface in its package; `linux.go` implements it, `mock.go` implements it deterministically.

```go
// internal/metrics/gpu/provider.go
type Provider interface {
    Devices(ctx context.Context) ([]Device, error)
    Sample(ctx context.Context) ([]Sample, error) // one reading per device
}
```

`yubeosd --demo` wires every provider to its mock with a seeded PRNG (`--demo-seed`), so screenshots, UI tests and docs are reproducible. Demo mode also replaces `yubeos-priv` with an in-process fake: **no root, ZFS or GPU is needed to run the whole product**. This is what makes development on Windows + WSL2 practical, where ZFS and GPUs are not available.

Provider selection is explicit (`providers.storage: auto|zfs|plain|mock`); `auto` probes the host once at start-up and logs the decision.

## 7. Real-time data flow

```
collector goroutine (1 Hz) ─▶ ring buffer per series (3 600 pts = 1 h @ 1 s)
                           └▶ events.Publish("metrics.sample", frame) ─▶ WS /ws/metrics
every 60 s: rollup (min/avg/max) ─▶ SQLite metrics_rollup (retained 30 d)
```

- Frame: JSON `{t, v:{"cpu.0.util":12.3, …}}` — flat series-key namespace (see [dashboard](modules/dashboard.md)).
- Back-pressure: bounded per-client queue; on overflow oldest frames are dropped and `{"dropped":n}` is sent. Metrics never block collectors.
- `GET /metrics/history` serves ≤ 1 h from the ring, longer ranges from rollups.

## 8. Long-running work: jobs and events

Anything that may exceed ~2 s (pool create, scrub, add-on install, archive build, catalogue refresh) is a **job**:

`POST …` → `202 {jobId}` → progress via `/ws/events` (`job.updated`) or `GET /api/v1/jobs/{id}`.

States: `queued → running → succeeded | failed | cancelled`. Jobs persist in SQLite. After a daemon restart, `running` jobs become `failed(reason=interrupted)`; idempotent ones offer *retry*. A job records `actor`, `kind`, redacted `params`, `progress`, `logTail`.

## 9. State ownership

| Data | Owner | Stored |
|---|---|---|
| Disks, partitions, SMART | Linux | read live; cached 30 s |
| Pools, vdevs, datasets, snapshots | ZFS | read live; cached 5 s; invalidated on job completion |
| Disk alias, tags | Yubeos | SQLite `disk_meta`, keyed by stable ID |
| SMB/NFS shares | Yubeos (intent) → rendered to `/etc/samba/yubeos.conf`, `/etc/exports.d/yubeos.exports` | SQLite `shares` |
| Add-ons | Yubeos (desired) + Docker (actual) | SQLite `addons_installed`, reconciled |
| Linux identity (uid, groups → role) | Linux | read live |
| Web credentials (Argon2id verifiers, TOTP secrets, passkeys, recovery hashes) | Yubeos, **inside `yubeos-priv`** | `/var/lib/yubeos-priv/credentials.db` (root, `0600`) — never readable by `yubeosd` |
| Sessions, trusted devices, throttling, bans | Yubeos (daemon) | SQLite `sessions`, `trusted_devices`, `ip_bans`, `login_failures` |
| Themes, preferences | Yubeos | SQLite + `/var/lib/yubeos/fonts` |
| Repo-Papers data | Yubeos | SQLite `rp_*` |

**Reconciliation:** every 60 s and on relevant events, `shares` and `addons` compare desired vs actual and emit a *drift* event (visible in UI) before correcting. Auto-correct is on for add-on container state, **off for destructive storage drift** — Yubeos never auto-destroys or auto-imports pools.

### 9.1 Stable device identity

Storage code addresses devices only via `/dev/disk/by-id/*` (WWN → `nvme-eui` → `ata-MODEL_SERIAL`; kernel path only as last resort). The API disk `id` is the first available of those. Rationale: `/dev/sdX` changes between boots; pools created on by-id names survive controller re-ordering.

## 10. Front-end architecture

- **Stack:** React 19, TypeScript (strict), Vite, React Router, TanStack Query (server state), Zustand (ephemeral UI state), TanStack Table, uPlot (charts), `@xterm/xterm`, Radix primitives (a11y), i18next (`en`, `it`), CSS Modules + design tokens. **No CSS framework, no component kit** ([ADR-0004](adr/0004-react-spa.md), [ADR-0011](adr/0011-theming.md)).
- **Structure:** `web/src/{app,shell,modules,components,lib,theme,i18n}`; one lazy-loaded route chunk per module.
- **API client:** generated from `api/openapi.yaml`; hand-written API types are forbidden.
- **Real-time:** one connection manager for `/ws/events` + `/ws/metrics`, exponential back-off, pauses when the tab is hidden.
- **State rules:** server data only in TanStack Query; Zustand only for ephemeral UI; persisted preferences go through `/appearance/preferences` (plus a pre-auth theme cache in `localStorage` to avoid a flash of wrong theme).
- **Accessibility:** WCAG 2.2 AA; keyboard-navigable tables/tree; `prefers-reduced-motion` honoured.

## 11. Configuration

`/etc/yubeos/config.yaml`, validated against `schemas/config.schema.json`, env override `YUBEOS_<PATH>` (json-tag path upper-cased and joined by `_`, e.g. `YUBEOS_SERVER_TLS_MODE`, `YUBEOS_AUTH_REQUIRE2FAFORADMINS`). **Implementation note (Phase 0):** the loader currently reads JSON (a YAML subset); the YAML decoder is tracked as TD-009, so the sample below must be written as JSON until then:

```yaml
server:     { listen: ":443", httpRedirect: ":80", exposure: lan, allowedHosts: [], behindProxy: false, trustedProxies: [],
              treatOverlayAsLocal: true, tls: { mode: selfsigned } }      # tls.mode: selfsigned | files | acme (+ acmeDomains, acmeEmail)
access:     { allowCidrs: [] }                                            # optional network allow-list for remote origins
auth:       { mode: native, adminGroup: yubeos-admin, userGroup: yubeos-user, require2fa: admins, trustedDeviceDays: 30,
              allowPasskeyAsSoleFactor: true, sessionIdleMinutes: 30, remoteIdleMinutes: 15, sessionMaxHours: 12,
              lockoutThreshold: 10, lockoutMinutes: 15, adminRemoteTrustedDeviceDays: 14, breachCheck: false }
providers:  { storage: auto, gpu: auto, power: auto }
files:      { roots: ["/mnt"], followSymlinks: false }
terminal:   { allowUsers: false, requireStepUpWhenRemote: true, maxSessionsPerUser: 5, detachedTtlMinutes: 30 }
addons:     { enabled: true, catalogs: [ { id: official, url: "https://github.com/serpis172/Yubeos-addons", verify: true } ] }
repopapers: { enabled: true, githubTokenFile: "", sources: [github, arxiv, semanticscholar, huggingface] }
logging:    { level: info, format: json }
```

Secrets are referenced by **file path** (`…TokenFile`), never inline; the file must be mode `0600`.

## 12. Errors, logging, observability

- **Errors:** RFC 9457 `application/problem+json`, stable `type` URIs (`https://yubeos.dev/problems/<code>`), `title`, `status`, `detail`, `instance`, optional `errors[]`. Internal errors never leak paths or command output to non-admins.
- **Logging:** `log/slog` JSON → journald; fields `ts level msg reqId user module`. The request ID is propagated to privileged calls and audit rows.
- **Self-metrics:** optional `/metrics` (Prometheus text), localhost-only by default.
- **Audit:** append-only table + journald mirror ([SECURITY §7](SECURITY.md#7-audit)).

## 13. Failure modes

| Failure | Behaviour |
|---|---|
| `yubeos-priv` down | Degraded banner; unprivileged read paths keep working; privileged actions → `503 priv-unavailable` |
| `zpool` / `smartctl` missing | Capability flag false; module hides unsupported actions with an explanatory empty state |
| SQLite locked / corrupt | WAL + `busy_timeout`; nightly `VACUUM INTO` backup (7 kept); corrupt ⇒ start read-only and instruct restore |
| Docker down | Add-ons page shows "engine unreachable"; desired state is preserved |
| Internet down | Repo-Papers serves stale cache with an age badge; nothing else affected |
| WS disconnect | Charts freeze with "reconnecting", gap back-filled from `/metrics/history` |

## 14. Compatibility and version probing

At start-up `yubeosd` records versions of OpenZFS, smartmontools, Samba, Docker, Compose and `nvidia-smi` (exposed in `GET /system/info`). Parsers are chosen by **capability, not distro name**. Notably, Ubuntu 24.04 ships OpenZFS 2.2.x, which has no `-j` JSON output for `zpool status`; the ZFS backend uses scriptable `-H -p` output where available plus a fixture-tested text parser for `zpool status`, and switches to `-j` when OpenZFS ≥ 2.3 is detected ([ADR-0006](adr/0006-zfs-primary.md)).

## 15. Trade-offs and what we will revisit

| Decision | Cost accepted | Revisit when |
|---|---|---|
| Two processes (daemon + helper) | IPC layer, more packaging | Never — security foundation |
| SQLite only | Single writer, no HA | Multi-node or > 10 k writes/s |
| Per-session fs workers ([files](modules/files.md)) | Spawn latency on first op | p95 list latency > 300 ms |
| `nvidia-smi` streaming instead of NVML | CSV parsing, extra process | Need MIG details / sub-second sampling |
| Compose CLI via helper instead of Docker SDK | Less typed control | Per-container streaming stats at scale |
| Layer on stock Ubuntu, no own ISO | Less control of base image | Demand for appliance install → `live-build` ISO (post-1.0) |
| In-memory 1 h ring | Short history lost on restart | Persistent 1 s history requested |
