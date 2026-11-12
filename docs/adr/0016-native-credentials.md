# ADR-0016: Native credentials (Argon2id + passkeys + TOTP), trusted devices, credential store in the helper

**Status:** Accepted · **Date:** 2026-11-12 · Supersedes ADR-0010

## Context
The web UI must be reachable from outside the local network with robust authentication, 2FA and remembered devices. ADR-0010 verified the real Linux password through PAM: on an internet-reachable login that turns a web brute-force into a system-password compromise, and it cannot carry passkeys or per-account lockout policy.

## Decision
- Web accounts are **linked to Linux accounts** (permissions and role from Linux uid/groups) but use **separate Yubeos credentials**: Argon2id password, WebAuthn passkeys, TOTP, recovery codes.
- Credential material is stored and verified by `yubeos-priv` (root-owned store); the daemon relays and holds only sessions, trusted-device hashes and throttling state.
- **Trusted devices**: opt-in, hashed, rotated per use with reuse detection, time-limited from the last full 2FA, skip only the TOTP prompt.
- Step-up tickets issued and verified by the helper.
- `pam` mode kept as an optional LAN-only mode (build tag `pam`, cgo).
- Libraries: stdlib + `golang.org/x/crypto` (argon2) + `github.com/go-webauthn/webauthn` (WebAuthn is protocol-heavy; not hand-rolled). TOTP is implemented in-house (RFC 6238, ~60 lines, test vectors).

## Consequences
+ Leaked web credentials do not grant OS access; passkeys give phishing resistance; consistent policy and audit.
+ A compromised daemon cannot dump credentials or mint step-up tickets.
− The helper gains a dependency (go-webauthn) and a small credential store; ADR-0013's "≤ 3 modules for the helper" is relaxed to "≤ 5, all reviewed" for this reason.
− Users manage a second password (mitigated by passkeys, which need no password).
− Passkeys are bound to the hostname; changing it requires re-enrollment.

## Alternatives considered
PAM only (rejected for remote exposure); external IdP/OIDC (planned post-1.0, would add a dependency on a third service); client TLS certificates (strong but poor UX across devices); email/SMS recovery (larger attack surface — rejected, recovery is by codes or host root).
