# Changelog

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com/), versioning: [SemVer](https://semver.org/).

## [Unreleased]

### Changed
- Repository identity: `github.com/serpis172/Yubeos`, Apache-2.0 confirmed. `auth.require2faForAdmins` replaced by `auth.require2fa: none|admins|all`. ADR-0010 superseded by ADR-0016.

### Added
- **Remote access & authentication design:** [AUTHENTICATION](docs/AUTHENTICATION.md) (native Argon2id credentials in the root helper, passkeys, TOTP, recovery codes, trusted devices with rotation/reuse detection, step-up, lockout/bans) and [REMOTE-ACCESS](docs/REMOTE-ACCESS.md) (overlay VPN / tunnel / direct, `exposure: remote` guard rails, ACME, origin classification); ADR-0016/0017; OpenAPI auth contract; config keys + validation + tests.
- Initial architecture, security model, API/data contracts, module specifications and roadmap.
- Phase 0 implementation: `yubeosd` (HTTPS, self-signed TLS, config, security middleware, default-deny auth spine, `/healthz`, `/api/v1/system/info`, `--demo`), embedded React shell with theme tokens and generated API client, Makefile, CI workflow, GitHub templates, Apache-2.0 license.
