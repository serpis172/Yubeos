# Remote Access (reaching Yubeos from outside the home network)

Status: **Accepted (v0.2 spec)** · Related: [AUTHENTICATION](AUTHENTICATION.md), [SECURITY](SECURITY.md), [DEPLOYMENT](DEPLOYMENT.md), [ADR-0017](adr/0017-remote-access.md)

## 1. Honest framing

Yubeos manages disks, files, containers and offers a shell. An internet-exposed instance is therefore a high-value target. Yubeos supports remote access **properly** — mandatory 2FA, trusted TLS, brute-force defences, device management, audit — but the safest design is still *not* to publish the login page to the whole internet. The three supported ways, from safest to most exposed:

| Option | How it works | Exposure | Recommended for |
|---|---|---|---|
| **A. Private overlay (VPN)** — Tailscale, WireGuard, ZeroTier | You (and your devices) join a private network; Yubeos is reachable only inside it | none to the internet | **Default recommendation.** Zero open ports; Yubeos works unchanged (overlay IPs count as *local*, see §4, unless you opt out) |
| **B. Tunnel / reverse proxy** — Cloudflare Tunnel, Caddy/Nginx, Tailscale Funnel | A connector makes an outbound connection; the proxy terminates TLS and forwards to Yubeos | public hostname, no port-forward; optional extra gate (e.g. Cloudflare Access) in front | You want a normal `https://nas.example.org` without a VPN client |
| **C. Direct** — port-forward 443 + DDNS + ACME certificate | Yubeos terminates TLS itself | public hostname **and** open port | Supported, most exposed; use with all hardening in §5 |

In all cases Yubeos' own login protections apply; options A/B/C only change who can *reach* the login page.

## 2. Exposure modes (`server.exposure`)

| Mode | Meaning | Rules |
|---|---|---|
| `lan` (default) | Intended for LAN/VPN. Remote-origin logins are still treated more strictly (§4). | Self-signed TLS allowed; PAM mode allowed; `require2fa` may be `admins` |
| `remote` | Reachable from the internet (options B or C). | **Enforced at startup — yubeosd refuses to start otherwise:** `allowedHosts` set (your public hostname) · trusted certificate (`tls.mode: files|acme`) **or** `behindProxy: true` with `trustedProxies` · `auth.mode: native` · `auth.require2fa: all` · `allowRoot: false` |

Example (direct, option C):

```json
{
  "server": { "exposure": "remote", "listen": ":443", "httpRedirect": ":80",
              "allowedHosts": ["nas.example.org"],
              "tls": { "mode": "acme", "acmeDomains": ["nas.example.org"], "acmeEmail": "me@example.org" } },
  "auth":   { "mode": "native", "require2fa": "all", "trustedDeviceDays": 30 }
}
```

Example (behind Cloudflare Tunnel or Caddy on the same host, option B):

```json
{
  "server": { "exposure": "remote", "listen": "127.0.0.1:8443", "behindProxy": true,
              "trustedProxies": ["127.0.0.1"], "allowedHosts": ["nas.example.org"] },
  "auth":   { "mode": "native", "require2fa": "all" }
}
```

## 3. Hard rules in remote mode (enforced by config validation and tests)

1. A hostname allow-list (`allowedHosts`) — defeats DNS rebinding and host-header attacks.
2. A certificate browsers trust (needed for secure cookies, HSTS and **passkeys**); self-signed is rejected.
3. 2FA for **every** account, no exceptions (`require2fa: all`); users without 2FA cannot sign in from outside ([AUTHENTICATION §4](AUTHENTICATION.md#4-login-flows)).
4. Native credentials only (no system password exposed to the internet).
5. No `root` web account.
6. HSTS (`max-age=31536000; includeSubDomains`) on every response; no mixed content.
7. `/healthz` is the only unauthenticated endpoint besides the login and static UI; the UI bundle contains no secrets or host details.

## 4. Origin classification and policy

Every request is classified by its **client address**:

| Class | Addresses |
|---|---|
| **local** | loopback, RFC 1918 (10/8, 172.16/12, 192.168/16), IPv6 ULA `fc00::/7`, link-local, **and (default) the Tailscale/CGNAT range `100.64.0.0/10`** (overlay members are authenticated by the VPN) |
| **remote** | everything else |

The client address is the TCP peer, **unless** the peer is in `trustedProxies` — only then are `X-Forwarded-For` / `Forwarded` (rightmost untrusted hop) and `CF-Connecting-IP` honoured. A header from an untrusted peer is ignored (tested). Overlay-as-local can be disabled with `server.treatOverlayAsLocal: false`.

| Policy | local origin | remote origin |
|---|---|---|
| 2FA at login | per `auth.require2fa` | **always** |
| Account without 2FA | may sign in **only to enrol** | refused (`enrollment-required-local`) |
| Idle timeout | 30 min | 15 min (`auth.remoteIdleMinutes`) |
| Trusted-device lifetime (admins) | `trustedDeviceDays` | ≤ 14 days |
| Terminal / destructive actions | terminal: no step-up; destructive: step-up | **step-up for both** |
| IP ban after repeated failures | never | yes ([AUTHENTICATION §8](AUTHENTICATION.md#8-brute-force-and-abuse-protection)) |
| Optional CIDR allow-list (`access.allowCIDRs`) | — | applied before any login logic |

## 5. Network-level hardening for option C (and B)

- **Firewall:** open only 443 (and 80 for HTTP-01 ACME, or use TLS-ALPN-01 on 443 / DNS-01). Nothing else of the NAS (SSH, SMB 445/139, NFS, Docker, add-on ports 20000–29999) must be forwarded. The installer's `yubeosctl doctor` warns when a UPnP/NAT mapping or listening socket suggests otherwise.
- **fail2ban:** `packaging/fail2ban/` ships a filter + jail on the `yubeos-auth` log lines.
- **Rate limits:** per-IP connection cap and request-rate limiter on all unauthenticated paths.
- **Optional pre-gate:** put Cloudflare Access / an OIDC proxy in front (option B) for a second, independent login; Yubeos still enforces its own.
- **Updates:** unattended security updates for the OS; Yubeos update notice in the UI.
- **Do not** expose add-on ports directly; reach add-ons through `/addons/<id>/` behind the Yubeos session.

## 6. TLS options

| Mode | Notes |
|---|---|
| `acme` | Built-in ACME client (`golang.org/x/crypto/acme/autocert`): HTTP-01 (needs :80) or TLS-ALPN-01 (needs :443 reachable by the CA). Renewal automatic, certificates cached in `/var/lib/yubeos/acme` (`0700`). DNS-01 providers (Cloudflare, etc.) are a v1.1 add-on for NAS behind CGNAT. |
| `files` | Bring your own certificate (e.g. from `certbot`, Caddy, Tailscale `cert`); reloaded on `SIGHUP`/file change. |
| `selfsigned` | LAN only; certificate fingerprint printed at first start; refused in remote mode. |
| `behindProxy` | TLS ends at the proxy/tunnel; Yubeos listens on loopback or a private interface and trusts only `trustedProxies`. |

## 6.1 Dynamic DNS and CGNAT

If your ISP uses CGNAT (no public IPv4), option C is impossible; use A or B. For a dynamic public IP, use a DDNS client of your choice (documented recipes: `ddclient`, Cloudflare API); Yubeos does not manage DNS in 1.0.

## 7. Remote-access readiness check

`yubeosctl doctor --remote` and *Settings → Security → Remote access* show a checklist, red/amber/green:

- exposure mode, hostname, certificate validity and expiry
- all accounts have 2FA; admins have a passkey (recommended)
- trusted proxies correct; client IP detection sanity test
- ports reachable from the internet (self-test via an optional external probe; off by default)
- fail2ban jail active; OS updates current; no default or weak admin password
- recent security events (lockouts, bans, new devices)

## 8. Incident response

- **Suspected compromise of a device:** `Settings → Security → Devices → Revoke` or `Sign out everywhere` (also forgets all trusted devices).
- **Suspected credential leak:** change password (revokes sessions/devices), regenerate recovery codes, rotate API tokens.
- **Active attack:** `sudo yubeosctl lockdown on` switches remote origins to *deny all* instantly (LAN/console access continues); `lockdown off` restores.
- **After an incident:** export `GET /api/v1/audit?format=ndjson`; the hash chain proves the log was not edited ([SECURITY §7](SECURITY.md#7-audit)).

## 9. Test plan (Phase 1d)

1. Forged `X-Forwarded-For` from an untrusted peer is ignored; from a trusted proxy it is honoured.
2. Remote-origin login without 2FA enrolment is refused; local-origin enrollment works.
3. Config validation rejects every unsafe remote combination (table in §2 is unit-tested).
4. ACME flow against a Pebble test CA in the VM lab; renewal path; port 80 redirect.
5. Rate limit / ban escalation under a simulated distributed guess (k6 script).
6. HSTS, CSP, cookie attributes verified by an automated header test against a remote-mode instance.
7. `lockdown on` blocks remote origins within 1 s, keeps local access.
