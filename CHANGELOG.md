# Changelog

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com/), versioning: [SemVer](https://semver.org/).

## [Unreleased]

### Added (Phase 2 backend slice + Add-ons list)
- `GET /api/v1/metrics/{catalog,snapshot,history}`: real host metrics from `/proc` (CPU, memory incl. ZFS ARC, network, disks, load, uptime), one-hour ring buffer; mock data in `--demo`. Per-minute min/avg/max rollups are kept in SQLite (`metrics_rollup`, migration 0002, 30 days) and answer the 24h/7d/30d ranges.
- `GET /ws/metrics` (WebSocket, one `{t,v}` frame per second, optional `?series=` filter; Origin-checked, closes when the session ends, capped at 64 connections). Hand-written RFC 6455 server, no new dependency.
- `GET/PUT /api/v1/me/preferences` (validated, stored in `settings`), `GET /api/v1/auth/factors`, `GET /api/v1/security/remote-status`.
- Add-ons panel: `GET /api/v1/addons` lists Yubeos-managed compose apps (read-only `compose.list` in `yubeos-priv`) and returns a safe URL per ADR-0018 (isolated `<id>.<addons.baseDomain>` origin or a LAN-IP direct link; same-host links are refused). The UI opens it with `target="_blank" rel="noopener noreferrer"`.
- Second factors (Phase 1c, first part): TOTP enrolment/confirm/remove, recovery codes (10, single use, regenerate), the 202 login challenge + `POST /auth/2fa/verify`, and step-up (`/auth/stepup/begin`, `/auth/stepup`, 5-minute window; required to remove TOTP or regenerate recovery codes). Secrets and verification live in `yubeos-priv` (`secret.key`, migration 0002 of `credentials.db`, per-user rate limit inside the helper). A user who enrolled TOTP is always asked for it, even where policy would not require it.
- Not yet: passkeys (WebAuthn), trusted devices, signed step-up tickets, lockdown — these answer `501 Not Implemented` instead of 404. The login/step-up challenge store is in memory (a daemon restart makes people sign in again).

### Changed
- Repository identity: `github.com/serpis172/Yubeos`, Apache-2.0 confirmed. `auth.require2faForAdmins` replaced by `auth.require2fa: none|admins|all`. ADR-0010 superseded by ADR-0016.

### Added (Phase 1a/1b)
- `yubeos-priv` root helper (typed RPC over a Unix socket, peer-credential check, root-only methods), `yubeosctl` (`user add/reset`, `doctor`, `audit verify`).
- Argon2id credential store with pepper, one-time setup/reset links, credential epochs; password policy.
- Login, logout, `/auth/me`, setup, password change, session list/revocation; `__Host-` session cookies with rotation, CSRF + Origin checks, account lockout, escalating IP bans, enrollment-only sessions, trusted-proxy aware client IP / origin classification, `access.allowCidrs`.
- SQLite layer with embedded migrations and pre-migration backups; hash-chained audit log; YAML configuration.
- Login and setup pages; systemd units, sysusers, fail2ban filter; end-to-end smoke test against the real binaries.

### Fixed
- `api/openapi.yaml` now describes every endpoint the web client calls; `api.gen.ts` is regenerated from it (the CI step "generated API client is up to date" was failing).
- Demo mode no longer panics on password change and device revocation (no session exists there).
- CI: Go version comes from `go.mod`; race detector and coverage run separately and coverage is limited to first-party code. `scripts/pin-actions.sh` refuses to write empty SHAs.
- Catalogue signature verification now defaults to **on** when `verify` is omitted (Go default and schema agree; a drift test guards all scalar defaults).
- The optional HTTP→HTTPS redirect can no longer take the HTTPS service down; it validates the Host header and uses a temporary redirect.
- `--demo` binds to loopback only and refuses public addresses without `--allow-demo-public`.
- Self-signed certificates renew at runtime; `files` certificates reload on `SIGHUP`/every 6 h; public IPs are no longer written into the certificate SANs.
- `tls.mode=acme` is rejected by validation until implemented; `allowedHosts`, `files.roots`, `terminal.shell`, `access.allowCidrs` are validated; unenforced settings log warnings at startup.
- CSP `connect-src` no longer allows arbitrary `wss:` hosts; added `object-src 'none'`, CORP, wider Permissions-Policy, HSTS in remote mode.
- OpenAPI: `/healthz` documented at the server root; all endpoints from API.md present; shared 403/422/429 responses; validator now checks `$ref`s, path parameters and add-on path-traversal fixtures.
- Frontend: `api()` handles 204/non-JSON/timeouts/cancellation; mobile navigation drawer; distinct "not signed in" vs "server unreachable" states with Retry; no theme flash; `system` theme; error boundary; self-hosted fonts.
- Add-on UI isolation decided (ADR-0018): never same origin/hostname as Yubeos.

### Added
- **Remote access & authentication design:** [AUTHENTICATION](docs/AUTHENTICATION.md) (native Argon2id credentials in the root helper, passkeys, TOTP, recovery codes, trusted devices with rotation/reuse detection, step-up, lockout/bans) and [REMOTE-ACCESS](docs/REMOTE-ACCESS.md) (overlay VPN / tunnel / direct, `exposure: remote` guard rails, ACME, origin classification); ADR-0016/0017; OpenAPI auth contract; config keys + validation + tests.
- Initial architecture, security model, API/data contracts, module specifications and roadmap.
- Phase 0 implementation: `yubeosd` (HTTPS, self-signed TLS, config, security middleware, default-deny auth spine, `/healthz`, `/api/v1/system/info`, `--demo`), embedded React shell with theme tokens and generated API client, Makefile, CI workflow, GitHub templates, Apache-2.0 license.
