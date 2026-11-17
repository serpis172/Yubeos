# ADR-0018: Add-on UIs never share Yubeos' origin or hostname

**Status:** Accepted · **Date:** 2026-11-13

## Context
The first design proxied add-on UIs under `/addons/<id>/` (same origin as Yubeos) with a CSP sandbox containing `allow-scripts allow-same-origin`. For same-origin content that combination removes the sandbox: the add-on's JavaScript runs with the admin's session cookie, can call `/api/v1/*` as the admin and read the CSRF token. Stripping cookies *upstream* is irrelevant because the browser attaches them to same-origin requests. A second subtlety: cookies are **not isolated by port**, so linking to `https://nas:20001` from `https://nas` would still send Yubeos' cookies to the add-on's server. Add-on code must be treated as untrusted.

## Decision
1. Yubeos **never serves add-on content from its own origin** and never forwards Yubeos cookies to add-ons.
2. **Isolated-subdomain proxy (preferred):** `https://<id>.<addons.baseDomain>` (e.g. `jellyfin.addons.nas.example.org`, wildcard DNS + wildcard certificate). Yubeos' session cookie is `__Host-` (host-only, `Path=/`), so it is never sent to sibling or child hostnames. Access is granted by a short-lived (60 s), single-use, add-on-scoped ticket exchanged by redirect, which sets a *separate* cookie valid only for that add-on hostname.
3. **Direct link (LAN fallback, `embed: link`):** the UI link uses a **different hostname than the one used to reach Yubeos** (e.g. the server IP when Yubeos was opened by name). Because a same-host different-port link would leak cookies, Yubeos refuses to generate a same-host link and shows the add-on's alternate address; the manager states this limitation.
4. `ui.embed: iframe` is only permitted with the isolated-subdomain mode; the iframe sandbox **never** includes `allow-same-origin` together with `allow-scripts` for anything served from Yubeos' origin (there is none).
5. In `exposure: remote`, add-on UIs are reachable **only** through the isolated-subdomain proxy (requires `addons.baseDomain`, a wildcard certificate and therefore DNS-01 ACME, planned v1.1, or a BYO wildcard certificate). Otherwise add-on UIs are LAN/VPN-only.
6. The Phase 7 proxy is not started before this decision is implemented; a test asserts that no response served under Yubeos' hostname contains add-on-controlled HTML.

## Consequences
+ A malicious or compromised add-on cannot act as the administrator through the browser.
− Needs a wildcard hostname/certificate for the best experience; LAN users without DNS use the direct-link fallback and its caveats.
− A small ticket-exchange handler and a second cookie scope to implement and test (Phase 7c).

## Alternatives considered
Same-origin proxy with sandbox minus `allow-same-origin` + postMessage bridge (breaks most apps that need storage/cookies); per-add-on API tokens with same-origin serving (does not stop XSS-style abuse of the admin session); only `embed: link` on same hostname (cookies leak across ports).
