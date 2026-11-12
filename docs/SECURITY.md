# Security

Status: **Accepted (v0.1 spec)** · Reporting vulnerabilities: see [§10](#10-vulnerability-disclosure)

## 1. Assets and adversaries

| Asset | Why it matters |
|---|---|
| Data on pools/datasets | Primary purpose of the machine |
| Root on the host | Control plane compromise = total compromise |
| Credentials (Linux accounts, 2FA secrets, API tokens) | Lateral movement |
| Add-on containers and their data | Frequently internet-facing, third-party code |

| Adversary | Capability assumed |
|---|---|
| A1 — Network attacker, unauthenticated (LAN, or the **internet** when `exposure: remote`) | Network reach to the login; automated credential guessing and stuffing; stolen passwords from other sites |
| A2 — Authenticated low-privilege user | Valid `yubeos-user` session |
| A3 — Malicious / compromised add-on | Code execution inside a container |
| A4 — Malicious web page visited by an admin | CSRF / DNS rebinding / WS hijack attempts |
| A5 — Compromised `yubeosd` | Arbitrary code as user `yubeos` |
| A6 — Supply chain | Poisoned dependency or release artifact |
| A7 — Device thief / malware on a user's browser | Steals a session cookie or a trusted-device cookie |

Out of scope: physical access to the server, a malicious root user, kernel exploits. Remote exposure is covered by [REMOTE-ACCESS](REMOTE-ACCESS.md).

## 2. Security architecture in one picture

`yubeosd` (network-facing, **unprivileged**) can only ask `yubeos-priv` (root, **no network**) to run a fixed set of typed operations. Compromise of the daemon (A5) must *not* yield arbitrary root execution, only the allow-listed operations — and the destructive subset additionally requires a fresh re-authentication proof the daemon cannot forge ([§4.3](#43-step-up-confirmation-for-destructive-operations)).

## 3. Authentication and sessions

Full specification: [AUTHENTICATION](AUTHENTICATION.md) (credentials, passkeys, TOTP, trusted devices, step-up, lockout) and [REMOTE-ACCESS](REMOTE-ACCESS.md) (exposure modes, TLS, client-IP trust). Summary of the security properties:

- **Identity:** a Yubeos web account linked to one Linux account (permissions follow the Linux account). Default `auth.mode: native` — a **separate web password** (Argon2id), so a leaked web password is not a system password. `pam` mode exists for LAN-only installs and is rejected in remote mode.
- **Credential material lives in `yubeos-priv`** (root-owned store), not in the daemon: password verifiers, TOTP secrets, passkey public keys, recovery hashes. The daemon relays.
- **Strong factors:** passkeys (WebAuthn, user verification required) — phishing-resistant and sufficient alone; or password + TOTP / passkey / recovery code. **2FA is mandatory for every non-local origin and for all accounts when `exposure: remote`.**
- **Trusted devices:** opt-in, hashed, rotated on every use with reuse detection, time-limited from the last full 2FA, skip *only* the TOTP prompt after a correct password; never satisfy step-up.
- **Sessions:** 256-bit ID (hash stored), `__Host-` cookie `Secure; HttpOnly; SameSite=Strict`, idle 30 min (15 min for remote origins), absolute 12 h, ID rotation; role/account re-validated continuously.
- **CSRF:** `SameSite=Strict` + `X-Yubeos-CSRF` header bound to the session on every non-GET; `Origin` check on all non-GET requests and WebSocket upgrades.
- **Brute force:** per-account soft lockout, per-IP escalating bans (remote origins only), global cool-down, dummy hashing for unknown users, fail2ban-ready logs.
- **API tokens:** `yb_<32 random bytes base64url>`, SHA-256 stored, scoped (`read`, `files`, `admin`), mandatory expiry ≤ 365 d, shown once; creation requires step-up.
- **Transport:** TLS 1.2+ (1.3 preferred); self-signed only in LAN mode; ACME/BYO certificate or trusted reverse proxy required for remote; HSTS on every response in remote mode; plain HTTP only redirects.

## 4. Privilege separation

### 4.1 Wire protocol

JSON-RPC 2.0 over a Unix stream socket `/run/yubeos/priv.sock` (mode `0660`, `root:yubeos`), length-prefixed frames (max 1 MiB). The server verifies `SO_PEERCRED` uid == `yubeos`. Each request carries `{reqId, actor, role, method, params}`.

### 4.2 Method allow-list policy

A method exists only if it has: (1) a typed params struct with validation, (2) an entry in `internal/privd/registry.go` declaring `class: read | mutate | destructive`, (3) an audit event definition, (4) a unit test with hostile input (path traversal, option injection, NUL bytes, over-long values).

Rules enforced by the framework, not by each handler:

- Binaries are resolved to **absolute paths at start-up** from a fixed list (`/usr/sbin/zpool`, `/usr/sbin/smartctl`, …).
- Arguments are passed as `[]string`; **never** through a shell; any value starting with `-` that is not an intended flag is rejected (`--` separator where supported).
- Environment is rebuilt from scratch (`PATH`, `LANG=C`); no inherited variables.
- Timeout per class (read 10 s, mutate 120 s, destructive 600 s) and stdout/stderr caps (4 MiB).
- Identifiers (pool names, dataset paths, disk IDs) are validated against strict regexes **and** must exist in a fresh inventory read before use.

Representative methods: `auth.password.{verify,set}`, `auth.totp.{enroll,verify}`, `auth.webauthn.{register,assert}`, `auth.recovery.verify`, `auth.stepup.issue`, `smart.read`, `smart.test.start`, `rapl.read`, `zfs.pool.{list,status,create,destroy,scrub,add,replace}`, `zfs.dataset.{create,set,destroy}`, `zfs.snapshot.{create,destroy,rollback}`, `shares.apply`, `compose.{up,down,restart,logs,ps}`, `pty.spawn`, `fsworker.spawn`.

### 4.3 Step-up confirmation for destructive operations

Destructive class (pool destroy, disk wipe, dataset destroy, snapshot rollback, user-data delete over a threshold, add-on removal with data) requires **both**:

1. UI typed confirmation (user types the resource name), and
2. a **step-up ticket**: the user re-verifies with password **and** a second factor (or a passkey with user verification) → `auth.stepup.issue` in the helper returns an HMAC-signed ticket `{user, sessionId, opClass, exp ≤ 5 min}` using a key only the helper holds. The helper verifies the ticket on every destructive or sensitive call. A trusted-device cookie never satisfies step-up ([AUTHENTICATION §7](AUTHENTICATION.md#7-step-up-re-authentication-for-dangerous-actions)).

A compromised daemon (A5) cannot read credential material (it lives in the helper store) and cannot mint tickets without the user's live factors, so it cannot destroy data or change credentials through the helper. *Residual risk:* a compromised daemon can capture a password and a TOTP code in transit while a user logs in and use them once within their 30 s validity; mitigated by single-use TOTP steps, short ticket lifetime, audit, and passkeys (whose assertions are bound to the origin and challenge and cannot be replayed).

### 4.4 Hardening of the two units

`yubeosd.service` (excerpt):

```ini
[Service]
User=yubeos
Group=yubeos
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=yes
RestrictRealtime=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallFilter=@system-service
SystemCallArchitectures=native
StateDirectory=yubeos
RuntimeDirectory=yubeos
ConfigurationDirectory=yubeos
# PrivateDevices intentionally omitted: GPU collectors read /dev/nvidia*
```

`yubeos-priv.service`: runs as root with a **reduced capability bounding set** (`CAP_SYS_ADMIN`, `CAP_CHOWN`, `CAP_DAC_OVERRIDE`, `CAP_FOWNER`, `CAP_SETUID`, `CAP_SETGID`, `CAP_KILL`, `CAP_SYS_RESOURCE`), `IPAddressDeny=any` (no network), `RestrictAddressFamilies=AF_UNIX AF_NETLINK`, `ProtectHome=read-only`, `PrivateTmp=yes`. `NoNewPrivileges` is **not** set on the helper **because user terminals must be able to `sudo`**; therefore terminal sessions are *not* spawned as children of the helper's sandbox but as transient systemd units ([terminal](modules/terminal.md), [ADR-0014](adr/0014-terminal-spawn.md)).

An AppArmor profile for `yubeosd` (deny write outside state/runtime dirs; deny exec except none) ships in `packaging/apparmor/`.

## 5. Authorization matrix

| Capability | user | admin |
|---|:-:|:-:|
| Dashboard / metrics read | ✔ | ✔ |
| File Explorer (own permissions, enforced by running as the user) | ✔ | ✔ (+ any path as self; no implicit root) |
| Terminal as self (step-up when the session origin is remote) | ✔ (if `terminal.allowUsers`) | ✔ |
| Disk Manager read | – | ✔ |
| Pools / datasets / shares mutate | – | ✔ (+ step-up for destructive) |
| Add-ons browse | ✔ | ✔ |
| Add-ons install / remove / configure | – | ✔ |
| Repo-Papers read | ✔ | ✔ |
| Repo-Papers sources & tokens | – | ✔ |
| Appearance (own preferences) | ✔ | ✔ |
| Appearance (global default theme) | – | ✔ |
| Audit log, sessions/trusted devices of others, tokens of others | – | ✔ |
| Remote-access settings, lockdown | – | ✔ (step-up) |

Authorization is enforced in **middleware by route metadata** (`requires: admin|user`), default-deny; handlers never check roles ad hoc.

## 6. Module-specific controls

| Module | Controls |
|---|---|
| Files | Operations run **as the requesting Linux user** (fs worker with `Credential{uid,gid,groups}`), so kernel permissions apply, not Yubeos logic. Root list (`files.roots`) is a UX boundary and an extra guard, not the only one. Path canonicalisation via `openat2(RESOLVE_BENEATH)` semantics (symlinks not followed by default). Download `Content-Disposition` sanitised; `X-Content-Type-Options: nosniff`; user files are never served inline as HTML (always `attachment`) to prevent stored XSS. |
| Terminal | Authenticated WS, bound to session; PTY runs as the user in a systemd transient unit; audit of open/close (not keystrokes by default; optional `terminal.recordSessions`). Admin shell ≈ root via sudo — documented, not hidden. |
| Storage | Disk IDs only; dry-run (`zpool create -n`) preview mandatory before create; boot/OS disk and in-use disks are *refused* for wipe/add; destructive ⇒ step-up. |
| Add-ons | Manifest schema validation; catalogue signature verification (minisign/cosign) for `official`; unsigned catalogues flagged; image digests pinned (`@sha256`) in first-party manifests; `privileged`, `network_mode: host`, `docker.sock`, `/dev` or host-path mounts outside add-on datasets are **blocked unless the manifest declares them and the admin approves** with a risk dialog. Add-on UIs are proxied under `/addons/<id>/` with a CSP sandbox and without Yubeos cookies forwarded. |
| Repo-Papers | Outbound allow-list per source; response size and time caps; all fetched text rendered as escaped text/Markdown-sanitised, never raw HTML; GitHub token read from `0600` file. |
| Appearance | Themes are **data only** (validated JSON; hex/rgb colours, font names, numeric ranges). No custom CSS/JS injection in v1; uploaded fonts validated (woff2 magic, ≤ 2 MB) and served with `font-src 'self'`. |

## 7. Audit

Append-only table `audit_log` (`id, ts, actor, role, ip, reqId, action, target, params(redacted), result, jobId`) mirrored to journald (`SYSLOG_IDENTIFIER=yubeos-audit`). Logged: logins (ok/fail), logout, step-up, every `mutate`/`destructive` privileged call, config changes, token create/revoke, terminal open/close, add-on lifecycle, share changes. Secrets/passwords/tokens are never logged. Retention default 365 d; export via `GET /api/v1/audit?format=ndjson`. Tamper-evidence: each row stores `prevHash = SHA-256(prevRow)` forming a hash chain; `yubeosctl doctor` verifies it.

## 8. HTTP hardening

```
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline';
  img-src 'self' data:; font-src 'self'; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'
X-Content-Type-Options: nosniff
Referrer-Policy: no-referrer
Permissions-Policy: camera=(), microphone=(), geolocation=()
Cross-Origin-Opener-Policy: same-origin
Cache-Control: no-store            (API)   |  immutable, hashed filenames (static)
```

In `remote` mode additionally: `Strict-Transport-Security: max-age=31536000; includeSubDomains`.

(`style-src 'unsafe-inline'` is required for runtime theme tokens set via `style` attributes; a nonce-based alternative is tracked in [TECH-DEBT](engineering/TECH-DEBT.md) as TD-001.) Add-on proxy pages use their own, stricter sandbox CSP. DNS-rebinding defence: `Host` header must match configured hostnames/IPs (`server.allowedHosts`).

## 9. Supply chain and release integrity

- Go: `go.sum` pinned, `govulncheck` in CI; npm: lockfile with `npm ci`, `--ignore-scripts`, `npm audit` + Dependabot weekly.
- Reproducible builds (`-trimpath`, fixed `SOURCE_DATE_EPOCH`), SBOM (CycloneDX) attached to every release, artifacts signed with **cosign** (keyless, GitHub OIDC); `.deb` and apt repository metadata signed; installer verifies checksums and signature.
- GitHub: branch protection, required reviews, signed tags, CodeQL, secret scanning, 2FA mandatory for maintainers.
- Dependency admission criteria: [ADR-0013](adr/0013-dependency-policy.md).

## 10. Vulnerability disclosure

Report privately through GitHub Security Advisories (`Security → Report a vulnerability`) or `security@serpis172-DOMAIN`. Acknowledgement within 72 h, triage within 7 days, coordinated disclosure target 90 days. Supported versions: latest minor + previous minor after 1.0. Please do not open public issues for vulnerabilities.

## 11. Residual risks (accepted, documented)

1. An admin with terminal + sudo is root. Yubeos does not pretend otherwise.
2. Self-signed TLS (LAN mode only) trains users to click through warnings; the installer prints the certificate fingerprint and documents ACME/Tailscale-cert alternatives.
3. Docker membership is root-equivalent; therefore Docker access exists only inside `yubeos-priv`.
4. A compromised daemon can read everything the `yubeos` user can (SQLite, session store) — hence short sessions and hashed tokens.
5. A stolen trusted-device cookie **plus** the password lets an attacker skip TOTP until the device expires, is revoked, or reuse is detected ([AUTHENTICATION §5](AUTHENTICATION.md#5-trusted-devices-remember-this-device)). Passkeys avoid this trade-off.
6. Publishing the login page to the internet always carries residual risk from unknown vulnerabilities in the web stack; the overlay-VPN option in [REMOTE-ACCESS](REMOTE-ACCESS.md) removes it.
