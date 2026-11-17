# ADR-0013: Dependency policy (stdlib first)

**Status:** Accepted · **Date:** 2026-11-11

## Context
Every dependency is attack surface and maintenance cost, especially in a root-adjacent project.

## Decision
Order of preference: stdlib → platform feature → already-present dependency → new dependency. A new dependency needs: maintained (release < 12 months), permissive licence (MIT/BSD/Apache/ISC/MPL), no cgo (except libpam), no post-install scripts, vulnerability history reviewed, justification in the PR. `yubeos-priv` aims for ≤ 3 third-party modules. Initial allow-list (backend): `modernc.org/sqlite`, `github.com/coder/websocket`, `gopkg.in/yaml.v3`, `github.com/go-webauthn/webauthn` (priv only, ADR-0016), `github.com/msteinert/pam` (priv only, optional `pam` build tag), `golang.org/x/crypto` (argon2), `golang.org/x/text` (NFKC normalisation of passwords), `golang.org/x/sys`. Frontend: React, Vite, `@fontsource-variable/{inter,jetbrains-mono,space-grotesk}` (self-hosted OFL fonts), `eslint-plugin-jsx-a11y`, TanStack Query/Table, Zustand, uPlot, `@xterm/xterm` (+addons), Radix primitives, i18next, `openapi-typescript`.

## Consequences
Small, auditable tree; occasionally more code to write ourselves (proc parsers, ring buffers, cron).

## Alternatives considered
gopsutil/cobra/viper/gorm: convenient but heavy for what is needed.
