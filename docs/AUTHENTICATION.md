# Authentication, Second Factors and Trusted Devices

Status: **Accepted (v0.2 spec)** · Supersedes the auth parts of [ADR-0010](adr/0010-auth.md) → [ADR-0016](adr/0016-native-credentials.md) · Related: [REMOTE-ACCESS](REMOTE-ACCESS.md), [SECURITY](SECURITY.md), [API](API.md), [DATA-MODEL](DATA-MODEL.md)

Goal: a login system that is safe to expose to the internet, usable daily (passkeys, "remember this device"), and recoverable without email or third parties.

## 1. Concepts

| Term | Meaning |
|---|---|
| **Account** | A Yubeos web account **linked to exactly one Linux account** (uid). The Linux account decides what the user can do on the machine (file permissions, terminal identity); Yubeos decides who may use the web UI. |
| **Role** | `admin` if the Linux account is in group `yubeos-admin`, `user` if in `yubeos-user`. No group ⇒ cannot sign in. Evaluated at every login and every 5 minutes for live sessions. |
| **Factor** | Something proving identity: *password* (knowledge), *passkey* (possession + biometric/PIN), *TOTP code* (possession), *recovery code* (one-time fallback). |
| **Session** | Server-side login state in a cookie, short-lived. |
| **Trusted device** | A browser that completed 2FA and was explicitly remembered: it may skip the **TOTP prompt** (never the password) for a limited time. |
| **Step-up** | A fresh re-verification (password + second factor) required for dangerous actions. |

## 2. Why not simply "log in with the Linux password"?

`auth.mode: pam` (Linux password via PAM) remains available **for LAN-only installs**, but the default is `native`, because an internet-reachable login that checks real system passwords turns a web brute-force into a system compromise. With `native`:

- The web password is **separate from the Linux password** (an attacker who phishes or guesses it still cannot SSH in or `sudo`).
- Passkeys and TOTP can be attached to the account (not possible with PAM alone).
- Password hashing parameters, lockout and rate limiting are under Yubeos' control.

`pam` mode is **rejected by config validation** when `server.exposure: remote`.

## 3. Credential storage and verification

**Principle:** credential material is stored and verified by the root helper `yubeos-priv`, not by the network-facing daemon. The daemon relays requests and never holds password verifiers or TOTP secrets at rest, so a compromised daemon cannot dump or forge them ([SECURITY §4.3](SECURITY.md#43-step-up-confirmation-for-destructive-operations)).

| Material | Where | Format |
|---|---|---|
| Password verifier | helper store `/var/lib/yubeos-priv/credentials.db` (root, `0600`) | **Argon2id**, `m=64 MiB, t=3, p=2`, 16-byte random salt, 32-byte output; parameters calibrated at install to ≥ 250 ms on the host (never below m=19 MiB, t=2) and re-hashed on login when parameters change; optional server-side pepper in `/var/lib/yubeos-priv/pepper.key` |
| TOTP secret | helper store | 160-bit secret, AES-256-GCM encrypted with `/var/lib/yubeos-priv/secret.key`; `lastUsedStep` stored for replay protection |
| Passkey public keys | helper store | credential ID, COSE public key, signature counter, transports, AAGUID, backup flags, label |
| Recovery codes | helper store | 10 codes × 80 bit, shown once, stored as SHA-256, single use |
| Sessions, trusted devices, throttling counters | daemon SQLite (`yubeos.db`) | hashed identifiers (SHA-256 of 256-bit random tokens) |

The helper exposes `auth.*` RPC methods (`auth.password.verify`, `auth.password.set`, `auth.totp.verify`, `auth.recovery.verify`, `auth.webauthn.*`, `auth.stepup.issue`, …). They are rate-limited inside the helper as well (defence in depth).

**Password policy:** minimum 12 characters (no composition rules), maximum 1 024, must differ from the username, checked against an embedded list of the 10 000 most common passwords; optional k-anonymity breach check (HIBP range API) when `auth.breachCheck: true` (off by default: outbound call). Unicode NFKC-normalised before hashing. Changing the password revokes all sessions except the current one, and all trusted devices.

**No user enumeration:** unknown usernames run a dummy Argon2id verification, identical response bodies and similar timing, identical lockout behaviour.

## 4. Login flows

```
                 ┌─────────────── passkey (usernameless, userVerification=required) ───────────────┐
                 │                                                                                    ▼
Browser ── username+password ─▶ [lockout / rate-limit checks] ─▶ password OK?
                                                              │ yes
                         trusted-device cookie valid for this user? ── yes ─▶ SESSION
                                                              │ no
                                    second factor required? ── no (policy none/admins & non-admin, local origin) ─▶ SESSION
                                                              │ yes
                                  202 {methods:[webauthn,totp,recovery], challengeId}
                                                              │
                          passkey assertion │ TOTP code │ recovery code ─▶ OK ─▶ [optional "trust this device"] ─▶ SESSION
```

1. **Passkey as sole factor** (`auth.allowPasskeyAsSoleFactor: true`, default): a passkey assertion with user verification (PIN/biometric) satisfies both factors. It is the **recommended** way to sign in: phishing-resistant, no password typed, works from phone/laptop/security key.
2. **Password + second factor**: the password is verified first; the second factor is a passkey (as 2FA), a TOTP code, or a recovery code.
3. **Challenge** (`challengeId`) is single-use, bound to the client IP prefix and user agent family, expires after 5 minutes, and allows 5 attempts.
4. **2FA policy** (`auth.require2fa`): `none` (not recommended), `admins` (default), `all`. In addition **always**, regardless of this setting:
   - any login from a **non-local origin** requires a second factor ([REMOTE-ACCESS §4](REMOTE-ACCESS.md#4-origin-classification-and-policy));
   - when `server.exposure: remote`, `auth.require2fa` must be `all` (config validation).
5. **Enrollment rule:** a user who must have 2FA but has none can sign in **only from a local origin** and is forced into an enrollment wizard (passkey and/or TOTP + recovery codes) before getting a working session. From a non-local origin the login is refused with `enrollment-required-local`.

### 4.1 TOTP

RFC 6238 SHA-1, 6 digits, 30 s period (maximum compatibility with Google Authenticator, Aegis, Bitwarden, 1Password…), ±1 step window, each time step accepted once (replay-proof), enrollment via `otpauth://` QR + manual key, active only after a confirming code. Recovery codes are issued at confirmation and regenerated (invalidating old ones) on demand with step-up.

### 4.2 Passkeys (WebAuthn)

- Relying Party ID = configured public hostname (`server.allowedHosts[0]`; in LAN mode the hostname used at enrollment — documented caveat: passkeys are bound to the hostname, so changing it requires re-enrollment).
- `userVerification: required`, `residentKey: preferred`, attestation `none`, algorithms ES256/EdDSA/RS256, sign-counter regression ⇒ credential flagged and login refused + audit alert.
- Multiple passkeys per account (labelled, listable, revocable, each with last-used time).
- Requires HTTPS with a name browsers accept (secure context); not available on a bare IP with a self-signed cert — another reason `remote` mode needs a real certificate.

## 5. Trusted devices ("remember this device")

After a successful second factor the user may tick **"Trust this device"**.

| Aspect | Design |
|---|---|
| Token | 256-bit random, cookie `__Host-yubeos_device` (`Secure; HttpOnly; SameSite=Strict; Path=/`, `Max-Age = trustedDeviceDays`). Server stores only `SHA-256(token)`. |
| Lifetime | `auth.trustedDeviceDays` (default **30**, max 90, `0` disables the feature) measured from the **last full second-factor verification** — a trusted device never renews itself: after N days the second factor is asked again. |
| What it skips | **Only the TOTP prompt** of a normal login, and **only after the password (or passkey) succeeded**. It never replaces the password, never satisfies step-up, never applies to first logins from a new account, never applies to recovery/credential changes. |
| Rotation | The token is **rotated on every use**. Presenting an already-rotated (old) token signals theft or cloning: that device record and all sessions from it are revoked, the user is notified at next login, an audit alert is written, and a full 2FA is demanded. |
| Binding | Record keeps user-agent family + major version and OS; a big change (browser family/OS) invalidates trust. Not bound to IP (mobile networks change IPs) but IP/time of every use are recorded. |
| Management | `Settings → Security → Devices`: label (editable), created, last used, last IP, browser/OS, expiry; **revoke one**, **revoke all / sign out everywhere**. Admins can revoke another user's devices. |
| Notifications | New-device sign-in raises an in-app notice at next login and an audit event (`auth.device.new`); webhook notification (ntfy/Gotify/generic) in v1.1. |
| Reset triggers | Password change, 2FA reset, recovery-code use, role change, Linux account locked/removed ⇒ all trusted devices of that user are revoked. |
| Policy | Trusting a device from a non-local origin is allowed but capped to `min(trustedDeviceDays, 14)` for **admin** accounts (configurable `auth.adminRemoteTrustedDeviceDays`, default 14). |

Threat honesty: if an attacker steals both the cookie (e.g. malware on the device) **and** knows the password, the trusted-device feature lets them skip TOTP. Mitigations: HttpOnly+rotation (reuse detection), short admin cap, step-up for dangerous actions, user-visible device list. Users who do not accept this trade-off set `trustedDeviceDays: 0` or use passkeys (which do not need the feature).

## 6. Sessions

- Cookie `__Host-yubeos_session`, random 256-bit ID (hash stored), `Secure; HttpOnly; SameSite=Strict`.
- Idle timeout 30 min (remote origin: 15 min default, `auth.remoteIdleMinutes`), absolute 12 h; ID rotated at login, at step-up and every 15 min of activity.
- Every request re-checks: session valid, user enabled, Linux account exists and is not locked, role unchanged (cached 60 s).
- WebSockets close when their session ends; `Sign out everywhere` closes all.
- CSRF: `SameSite=Strict` + `X-Yubeos-CSRF` header bound to the session; `Origin` check on all non-GET and on WebSocket upgrades.

## 7. Step-up (re-authentication for dangerous actions)

A **step-up window of 5 minutes** is opened by `POST /auth/stepup` (password **and** a second factor; a passkey with user verification alone also qualifies). Trusted devices do **not** bypass it. The helper issues the window as an HMAC-signed ticket `{user, sessionId, class, exp}` that `yubeos-priv` itself verifies on privileged calls.

| Action | Step-up |
|---|---|
| Destroy pool/dataset, wipe disk, rollback, delete add-on data | **required** |
| Create/delete API tokens, change password, register/remove passkey or TOTP, regenerate recovery codes, revoke others' sessions | **required** |
| Open a **terminal** when the session origin is non-local (`terminal.requireStepUpWhenRemote`, default true) | **required** |
| Install add-on with elevated permissions, change sharing/exposure settings, change `exposure`/TLS | **required** |
| Normal read/write file operations, dashboard, add-on start/stop | not required |

## 8. Brute-force and abuse protection

| Layer | Rule (defaults, configurable) |
|---|---|
| Per account | exponential delay after failures; **soft lockout** of password logins for `lockoutMinutes` (15) after `lockoutThreshold` (10) consecutive failures. Passkey login and local-origin console recovery are unaffected, so an attacker cannot lock the owner out of passkey sign-in. |
| Per IP | 20 failures / 10 min ⇒ IP banned 1 h (escalating ×2 per repeat, cap 24 h); local origins are never banned. |
| Global | If failures exceed 100 / 10 min across all IPs, login requires a proof-of-work-free "cool-down" (all password logins delayed by 2 s) and an alert is raised. |
| Timing | Constant-time comparisons; dummy hashing for unknown users. |
| Logging | Structured failure lines `yubeos-auth fail user=<u> ip=<ip> reason=<r>` to journald; packaged **fail2ban filter** (`packaging/fail2ban/`) lets the OS firewall drop offenders. |
| Request limits | Body ≤ 4 KiB on auth endpoints; no auth endpoint accepts GET with secrets. |

## 9. Bootstrap, recovery and administration

- **First admin:** on the machine, `sudo yubeosctl user add <linux-user> --admin` prints a **one-time setup link** (valid 1 h, single use, honoured only from a local origin). The user sets a password and enrols a passkey/TOTP in the browser. No default accounts, no default passwords.
- **Add users:** admins invite Linux users from the UI (creates a setup link with the same constraints).
- **Lost second factor / password:** no email recovery (smaller remote attack surface). Use recovery codes; otherwise root on the host runs `sudo yubeosctl user reset <user> [--password] [--2fa]`, which issues a new local setup link and revokes sessions and trusted devices.
- **Lost all admins:** same root command; the host console is the root of trust.
- **Account lifecycle:** disabling the Linux account or removing it from the Yubeos groups revokes access within 5 minutes.

## 10. Audit events

`auth.login.ok|fail|denied`, `auth.2fa.ok|fail`, `auth.passkey.register|remove|counter-regression`, `auth.totp.enroll|confirm|remove`, `auth.recovery.use|regenerate`, `auth.device.new|trust|rotate|reuse-detected|revoke`, `auth.session.revoke`, `auth.stepup.ok|fail`, `auth.password.change`, `auth.lockout`, `auth.ipban`, `auth.setup-link.create|consume`. Each carries user, IP, user-agent family, request ID, origin class (local/remote), and result; never secrets.

## 11. Acceptance criteria (Phase 1b–1c)

1. Unit/integration tests for every row of §4–§8, including: replayed TOTP code rejected; reused rotated device token revokes the device; passkey counter regression refused; unknown-user vs wrong-password responses indistinguishable (body, status, timing within noise); lockout does not affect passkey login; step-up not satisfied by trusted device.
2. Argon2id calibration test on a low-power target (≤ 300 ms login on a 2-core NAS CPU) and hostile-input suite for every `auth.*` RPC method.
3. With `exposure: remote`, a user without 2FA cannot obtain a session from a non-local address (test via forged trusted-proxy headers from an untrusted peer ⇒ header ignored).
4. Fuzzing of WebAuthn and TOTP request parsing; no panic, bounded memory.
5. Manual security review of the whole flow with [CODE-REVIEW](engineering/CODE-REVIEW.md) §1 before tagging `v0.1.0`.
