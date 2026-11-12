# ADR-0017: Remote access via exposure modes with enforced safety rules

**Status:** Accepted · **Date:** 2026-11-12

## Context
Users want to reach Yubeos from outside the LAN. Safe configuration is easy to get wrong (self-signed cert on a public name, no 2FA, trusting spoofed `X-Forwarded-For`).

## Decision
A single switch `server.exposure: lan|remote`. `remote` makes `yubeosd` **refuse to start** unless: `allowedHosts` set, trusted TLS (`files|acme`) or `behindProxy` with `trustedProxies`, `auth.mode: native`, `auth.require2fa: all`, no root account. Client origin is classified local/remote; remote origins always need 2FA, get shorter sessions, step-up for terminal/destructive actions and IP bans. Client IP headers are trusted only from configured proxies. Three documented access options: overlay VPN (recommended), tunnel/reverse proxy, direct port-forward with ACME. Built-in ACME (autocert), fail2ban filter, `lockdown` command, readiness check.

## Consequences
+ Unsafe remote setups are impossible by construction and covered by unit tests.
+ Overlay-VPN users get zero exposure with no extra work.
− More configuration surface and test matrix; ACME needs ports 80/443 reachable (DNS-01 deferred to v1.1).
− Direct exposure still carries inherent residual risk (documented in SECURITY §11).

## Alternatives considered
Leaving exposure entirely to the user's reverse proxy (no guard rails); mandatory VPN only (too restrictive for the stated requirement); built-in tunnel service (operational burden, third-party trust).
