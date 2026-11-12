# HTTP API (v1)

Machine-readable source of truth: [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.1). This document is the human overview and the conventions every endpoint must follow.

## 1. Conventions

| Topic | Rule |
|---|---|
| Base path | `/api/v1` (breaking change ⇒ `/api/v2`; additive changes are non-breaking) |
| Format | JSON (`application/json`), `camelCase` fields, RFC 3339 UTC timestamps, sizes in bytes, rates in bytes/s |
| Auth | Session cookie + `X-Yubeos-CSRF` (browser) **or** `Authorization: Bearer yb_…` (API tokens, no CSRF needed) |
| Errors | RFC 9457 `application/problem+json`; `type` = `https://yubeos.dev/problems/<code>`; validation errors in `errors: [{pointer, message}]` |
| IDs | Opaque strings; disks use the stable device ID; pools/datasets use their ZFS name (URL-encoded) |
| Pagination | `?limit=&cursor=`; response `{items:[…], nextCursor}` |
| Sorting/filtering | `?sort=field,-field`, explicit filter params per endpoint |
| Idempotency | Mutating `POST` accepting `Idempotency-Key` header (24 h dedupe) |
| Async | Operations > ~2 s return `202 {jobId}` + `Location: /api/v1/jobs/{id}` |
| Concurrency | `ETag`/`If-Match` on mutable resources (`412` on mismatch) |
| Rate limits | Login and expensive endpoints; `429` + `Retry-After` |
| Compression | gzip/br for JSON > 1 KB |

Common problem codes: `unauthenticated`(401), `forbidden`(403), `reauth-required`(403, header `X-Yubeos-Reauth: required`), `locked-out`(429), `ip-banned`(403), `not-found`(404), `conflict`(409), `validation`(422), `precondition-failed`(412), `rate-limited`(429), `priv-unavailable`(503), `capability-missing`(501), `plan-expired`(409).

## 2. Endpoint overview

### System
| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | unauthenticated liveness (`{"ok":true}`) |
| GET | `/system/info` | host, kernel, OS, uptime, versions (zfs, smartctl, samba, docker…), capabilities, demoMode |
| GET | `/jobs`, `/jobs/{id}` · POST `/jobs/{id}/cancel` | job registry |
| GET | `/audit` | admin; filters `from,to,actor,action`; `format=ndjson` export |

### Auth ([AUTHENTICATION](AUTHENTICATION.md))
| Method | Path | Notes |
|---|---|---|
| POST | `/auth/login` | `{username, password}` → `200` session (trusted device / policy allows) or `202 {needs2fa, methods[], challengeId, enrollmentRequired?}` |
| POST | `/auth/2fa/verify` | `{challengeId, method: totp|recovery, code, trustDevice?, deviceLabel?}` → session |
| POST | `/auth/webauthn/login/begin` · `/finish` | passkey login (sole factor when `allowPasskeyAsSoleFactor`) or 2FA step |
| POST | `/auth/logout` | |
| GET | `/auth/me` | user, role, csrf token, factors, `remoteSession`, `trustedDevice` |
| POST | `/auth/stepup` | password + second factor (or passkey) → 5-minute step-up window |
| PUT | `/auth/password` | step-up; revokes other sessions and all trusted devices |
| POST | `/auth/totp/enroll` · `/auth/totp/confirm` · DELETE `/auth/totp` | confirm returns recovery codes once; step-up |
| POST | `/auth/webauthn/register/begin` · `/finish` · DELETE `/auth/webauthn/{id}` | passkeys; step-up |
| POST | `/auth/recovery-codes:regenerate` | step-up |
| GET/DELETE | `/auth/devices[/{id}]` · POST `/auth/devices:revokeAll` | trusted devices + sessions of the caller; admin may pass `?user=` |
| POST | `/auth/setup` | consume a one-time setup link (local origin only) to set the first password / enrol factors |
| GET/POST/DELETE | `/auth/tokens[/{id}]` | API tokens (step-up to create) |
| GET | `/security/remote-status` | admin: readiness checklist ([REMOTE-ACCESS §7](REMOTE-ACCESS.md#7-remote-access-readiness-check)) |
| POST | `/security/lockdown` | admin, step-up: `{enabled}` deny all remote origins |

Login responses never reveal whether a username exists. Problem codes added: `enrollment-required-local`, `locked-out`, `ip-banned`, `second-factor-required`, `challenge-expired`, `device-revoked`.

### Metrics
| Method | Path | Notes |
|---|---|---|
| GET | `/metrics/catalog` | series metadata (unit, label, group), devices |
| GET | `/metrics/snapshot` | latest value of every series |
| GET | `/metrics/history?series=a,b&range=5m|1h|24h|7d|30d&step=` | ring (≤ 1 h) or rollups |
| WS | `/ws/metrics?series=…` | frames `{t, v:{key:number}}`, `{dropped:n}` |

### Storage
| Method | Path | Notes |
|---|---|---|
| GET | `/storage/disks` · `/storage/disks/{id}` | inventory, derived tags, status |
| PATCH | `/storage/disks/{id}` | `{alias?, tags?}` (user tags only) |
| GET | `/storage/disks/{id}/smart` · POST `…/smart/tests` | job |
| GET | `/storage/pools` · `/storage/pools/{name}` | topology, health, scan, capacity |
| POST | `/storage/pools:plan` | dry-run → `{planId, topology, warnings, commandPreview}` |
| POST | `/storage/pools` | `{planId}` + step-up → job |
| POST | `/storage/pools/{name}:scrub` · `:replace` · `:export` · DELETE | destructive ⇒ step-up |
| GET/POST/PATCH/DELETE | `/storage/datasets[/{path}]` | |
| GET/POST/DELETE | `/storage/snapshots[/{name}]` · POST `…:rollback` | |
| GET/POST/PATCH/DELETE | `/shares[/{id}]` | SMB/NFS definitions |
| GET | `/shares/status` | service state + connections |

### Files
| Method | Path | Notes |
|---|---|---|
| GET | `/files/roots` | visible roots for the user |
| GET | `/files/list?root=&path=&limit=&cursor=&sort=` | |
| GET | `/files/download?root=&path=` | `Range` supported; `attachment` |
| POST | `/files/archive` | `{root, paths[]}` → streamed ZIP |
| PUT | `/files/upload?root=&path=&name=&overwrite=` | raw streaming body |
| POST | `/files/mkdir` · `/files/rename` · `/files/delete` | |

### Terminal
| Method | Path | Notes |
|---|---|---|
| POST | `/terminal/sessions` | `{cols, rows, title?}` → `{id}` |
| GET/DELETE | `/terminal/sessions[/{id}]` | list/kill |
| WS | `/ws/terminal/{id}` | binary + JSON control frames ([protocol](modules/terminal.md#2-wire-protocol-wsterminalid)) |

### Add-ons
| Method | Path | Notes |
|---|---|---|
| GET | `/addons/catalog?q=&category=` · `/addons/catalog/{id}` | |
| POST | `/addons/catalogs/refresh` · GET/POST/DELETE `/addons/catalogs` | |
| GET | `/addons` · `/addons/{id}` | installed + status |
| POST | `/addons` | `{id, version, config, approvals}` → job |
| POST | `/addons/{id}:start|stop|restart|update` | job |
| PATCH | `/addons/{id}/config` | |
| DELETE | `/addons/{id}?deleteData=` | `deleteData=true` ⇒ step-up |
| GET | `/addons/{id}/logs` | SSE tail |
| ANY | `/addons/{id}/ui/…` | authenticated reverse proxy (not part of the JSON API) |

### Repo-Papers
| Method | Path | Notes |
|---|---|---|
| GET | `/repopapers/repos?window=7d|30d|90d|180d&topic=&sort=trending|stars|new` | |
| GET | `/repopapers/papers?window=&topic=&sort=hot|cited|new&tier=` | |
| GET | `/repopapers/items/{id}/score` | breakdown |
| GET/POST/PATCH/DELETE | `/repopapers/topics[/{id}]` | |
| PUT | `/repopapers/items/{id}/state` | `{saved?, hidden?, read?}` |
| GET/PATCH | `/repopapers/sources` | admin |
| POST | `/repopapers/refresh` | admin, job |

### Appearance
| Method | Path | Notes |
|---|---|---|
| GET/PUT | `/appearance/preferences` | per-user (theme id, units, language, layout) |
| GET/PUT | `/appearance/defaults` | admin global default |
| GET/POST | `/appearance/themes` · GET/PUT/DELETE `/appearance/themes/{id}` | custom themes; built-ins read-only |
| POST | `/appearance/themes:validate` | schema validation w/o saving |
| POST/GET/DELETE | `/appearance/fonts[/{id}]` | `.woff2` upload |

## 3. WebSocket channels

| Path | Purpose | Messages |
|---|---|---|
| `/ws/events` | jobs, drift, alerts, disk/pool state changes, add-on status | `{type, ts, data}` (`job.updated`, `storage.changed`, `addon.status`, `alert.raised`, `drift.detected`) |
| `/ws/metrics` | live series | see above |
| `/ws/terminal/{id}` | PTY | see terminal spec |

All WS: `Origin` check, session required, server ping every 20 s, max message 1 MiB (terminal binary 64 KiB chunks).

## 4. Example

```http
POST /api/v1/storage/pools:plan
{ "name": "tank", "layout": "raidz2", "disks": ["wwn-0x5000c500a1b2c3d4", "…"], "options": { "compression": "lz4", "ashift": 12 } }

200 { "planId": "pl_9fK…", "expiresAt": "2026-11-11T08:10:00Z",
      "topology": { "vdevs": [ { "type": "raidz2", "disks": [ … ] } ] },
      "warnings": ["Mixed disk sizes: usable capacity limited by the smallest (4 TB)"],
      "commandPreview": "zpool create -o ashift=12 -O compression=lz4 -O atime=off tank raidz2 …" }
```

## 5. Contract testing

CI generates the TypeScript client from `openapi.yaml`, validates every handler response against the spec in integration tests (schema validation middleware enabled in test mode), and fails on spec/handler drift.
