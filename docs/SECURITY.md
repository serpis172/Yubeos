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
| A1 — LAN attacker, unauthenticated | Network reach to the web port |
| A2 — Authenticated low-privilege user | Valid `yubeos-user` session |
| A3 — Malicious / compromised add-on | Code execution inside a container |
| A4 — Malicious web page visited by an admin | CSRF / DNS rebinding / WS hijack attempts |
| A5 — Compromised `yubeosd` | Arbitrary code as user `yubeos` |
| A6 — Supply chain | Poisoned dependency or release artifact |

Out of scope: physical access, a malicious root user, kernel exploits.

## 2. Security architecture in one picture

`yubeosd` (network-facing, **unprivileged**) can only ask `yubeos-priv` (root, **no network**) to run a fixed set of typed operations. Compromise of the daemon (A5) must *not* yield arbitrary root execution, only the allow-listed operations — and the destructive subset additionally requires a fresh re-authentication proof the daemon cannot forge ([§4.3](#43-step-up-confirmation-for-destructive-operations)).

## 3. Authentication and sessions

- **Identity = Linux account** verified via PAM (`/etc/pam.d/yubeos`) **inside `yubeos-priv`** (root is required to verify arbitrary users). Yubeos never stores or hashes passwords.
- **Authorization groups:** `yubeos-admin` → role *admin*; `yubeos-user` → role *user*. No group → login refused. Root login is refused by default (`auth.allowRoot: false`).
- **Sessions:** 256-bit random ID, server-side in SQLite, cookie `__Host-yubeos_session` (`Secure; HttpOnly; SameSite=Strict; Path=/`). Idle timeout 30 min, absolute 12 h, rotated on login and privilege step-up, revocable per session from the UI.
- **CSRF:** `SameSite=Strict` **plus** mandatory `X-Yubeos-CSRF` header (value bound to the session) on every non-GET request.
- **WebSocket:** `Origin` must equal the served origin; session cookie required; per-message size caps.
- **2FA:** TOTP (RFC 6238), recovery codes (10, single-use, stored as SHA-256), optionally enforced for admins. Recovery by local root: `yubeosctl reset-2fa <user>`.
- **API tokens:** for automation, `yb_<random 32 bytes base64url>`, stored as SHA-256 only, scoped (`read`, `files`, `admin`), expiry required (max 365 d), shown once.
- **Brute force:** per-IP and per-account exponential back-off (5 failures → 1 min … cap 15 min), constant-time comparisons, uniform error text.
- **Transport:** TLS 1.2+ only (1.3 preferred); self-signed ECDSA P-256 generated at first start, SAN = hostname + IPs; ACME and BYO certs supported; HSTS only when a trusted certificate is configured. Plain HTTP only redirects.

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

Representative methods: `pam.authenticate`, `pam.reauth`, `smart.read`, `smart.test.start`, `rapl.read`, `zfs.pool.{list,status,create,destroy,scrub,add,replace}`, `zfs.dataset.{create,set,destroy}`, `zfs.snapshot.{create,destroy,rollback}`, `shares.apply`, `compose.{up,down,restart,logs,ps}`, `pty.spawn`, `fsworker.spawn`.

### 4.3 Step-up confirmation for destructive operations

Destructive class (pool destroy, disk wipe, dataset destroy, snapshot rollback, user-data delete over a threshold, add-on removal with data) requires **both**:

1. UI typed confirmation (user types the resource name), and
2. a **re-auth token**: the user re-enters their password → `pam.reauth` in the helper returns an HMAC-signed token `{user, opClass, exp ≤ 5 min}` using a key only the helper holds. The helper verifies the token on the destructive call.

A compromised daemon (A5) without the user's password cannot mint tokens, so it cannot destroy data through the helper. *Residual risk:* a compromised daemon that captures a password in transit; mitigated by short token lifetime, audit, and TLS termination before the daemon only in the same host.

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
| Terminal as self | ✔ (if `terminal.allowUsers`) | ✔ |
| Disk Manager read | – | ✔ |
| Pools / datasets / shares mutate | – | ✔ (+ step-up for destructive) |
| Add-ons browse | ✔ | ✔ |
| Add-ons install / remove / configure | – | ✔ |
| Repo-Papers read | ✔ | ✔ |
| Repo-Papers sources & tokens | – | ✔ |
| Appearance (own preferences) | ✔ | ✔ |
| Appearance (global default theme) | – | ✔ |
| Audit log, sessions of others, tokens of others | – | ✔ |

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

(`style-src 'unsafe-inline'` is required for runtime theme tokens set via `style` attributes; a nonce-based alternative is tracked in [TECH-DEBT](engineering/TECH-DEBT.md) as TD-001.) Add-on proxy pages use their own, stricter sandbox CSP. DNS-rebinding defence: `Host` header must match configured hostnames/IPs (`server.allowedHosts`).

## 9. Supply chain and release integrity

- Go: `go.sum` pinned, `govulncheck` in CI; npm: lockfile with `npm ci`, `--ignore-scripts`, `npm audit` + Dependabot weekly.
- Reproducible builds (`-trimpath`, fixed `SOURCE_DATE_EPOCH`), SBOM (CycloneDX) attached to every release, artifacts signed with **cosign** (keyless, GitHub OIDC); `.deb` and apt repository metadata signed; installer verifies checksums and signature.
- GitHub: branch protection, required reviews, signed tags, CodeQL, secret scanning, 2FA mandatory for maintainers.
- Dependency admission criteria: [ADR-0013](adr/0013-dependency-policy.md).

## 10. Vulnerability disclosure

Report privately through GitHub Security Advisories (`Security → Report a vulnerability`) or `security@OWNER-DOMAIN`. Acknowledgement within 72 h, triage within 7 days, coordinated disclosure target 90 days. Supported versions: latest minor + previous minor after 1.0. Please do not open public issues for vulnerabilities.

## 11. Residual risks (accepted, documented)

1. An admin with terminal + sudo is root. Yubeos does not pretend otherwise.
2. Self-signed TLS trains users to click through warnings; the installer prints the certificate fingerprint and documents ACME/Tailscale-cert alternatives.
3. Docker membership is root-equivalent; therefore Docker access exists only inside `yubeos-priv`.
4. A compromised daemon can read everything the `yubeos` user can (SQLite, session store) — hence short sessions and hashed tokens.
