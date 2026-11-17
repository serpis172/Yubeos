# Changelog

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com/), versioning: [SemVer](https://semver.org/).

## [Unreleased]

### Changed
- Repository identity: `github.com/serpis172/Yubeos`, Apache-2.0 confirmed. `auth.require2faForAdmins` replaced by `auth.require2fa: none|admins|all`. ADR-0010 superseded by ADR-0016.

### Added (Phase 1a/1b)
- `yubeos-priv` root helper (typed RPC over a Unix socket, peer-credential check, root-only methods), `yubeosctl` (`user add/reset`, `doctor`, `audit verify`).
- Argon2id credential store with pepper, one-time setup/reset links, credential epochs; password policy.
- Login, logout, `/auth/me`, setup, password change, session list/revocation; `__Host-` session cookies with rotation, CSRF + Origin checks, account lockout, escalating IP bans, enrollment-only sessions, trusted-proxy aware client IP / origin classification, `access.allowCidrs`.
- SQLite layer with embedded migrations and pre-migration backups; hash-chained audit log; YAML configuration.
- Login and setup pages; systemd units, sysusers, fail2ban filter; end-to-end smoke test against the real binaries.

### Fixed
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
