# Development Guide

## 1. Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | ≥ 1.22 (CI uses 1.24) | `yubeosd` is pure Go; `yubeos-priv` needs cgo + `libpam0g-dev` (Linux only) |
| Node.js | ≥ 22 LTS | + `npm` (lockfile enforced) |
| make, git | any | |
| Docker | optional | only to test add-ons for real |
| QEMU/KVM or Multipass/Lima | optional | VM lab for storage/terminal/priv integration tests |

## 2. Develop on any machine, including Windows + WSL2

The whole product runs in **demo mode** with mock hardware — no root, ZFS, GPU or Docker needed:

```bash
git clone https://github.com/OWNER/yubeos && cd yubeos
make deps            # go mod download + npm ci
make demo            # builds the UI, then runs yubeosd --demo on https://localhost:8443 (self-signed cert: accept the warning)
# UI hot-reload: terminal 1 `go run ./cmd/yubeosd --demo --no-tls`, terminal 2 `make dev-web` → http://localhost:5173
```

WSL2 specifics: work inside the Linux filesystem (`~/src`, not `/mnt/c`) for file-watcher speed; WSL2 has no ZFS kernel module and no SMART/GPU passthrough, so storage/GPU/power UIs use mocks there. Real-hardware behaviour is verified in the VM lab (§4) and nightly CI.

## 3. Make targets

| Target | Does |
|---|---|
| `make dev` | backend with live reload (air-style) + Vite, real providers where available |
| `make demo` | everything mocked, deterministic |
| `make test` | `go test ./... -race` + `vitest` |
| `make lint` | `golangci-lint`, `go vet`, ESLint, Stylelint (`no-raw-colors`), `tsc --noEmit`, OpenAPI + JSON-Schema lint |
| `make gen` | regenerate TS client from OpenAPI, Go types from schemas |
| `make build` | embeds `web/dist` and builds static binaries into `dist/` |
| `make deb` | builds `.deb` via nfpm |
| `make vm-test` | provisions the VM lab and runs integration tests |
| `make docs` | link-check and render docs |

## 4. VM lab

`test/vm/` contains a cloud-init + Multipass/QEMU recipe creating an Ubuntu 24.04 VM with **6 virtual disks** (for ZFS layouts), Samba, Docker, a second Linux user, and `yubeos` installed from the freshly built `.deb`. Fixture capture script `test/tools/capture-fixtures.sh` stores raw outputs of `zpool`, `zfs`, `smartctl -j`, `lsblk -J`, `nvidia-smi` into `test/fixtures/<tool>/<version>/…` (sanitised: serials replaced).

## 5. Conventions

**Go:** `gofmt`/`goimports`, `internal/` layering (modules must not import each other — only `events`, `jobs`, `priv`, `db`, `audit`); errors wrapped with `%w`; `context.Context` first param; no globals except `slog`; tables-driven tests; `-race` always; interfaces defined where *consumed*; no `init()` side effects.
**TypeScript:** strict mode, no `any` (lint error), feature folders, hooks per query, components < 200 lines, no default exports except routes.
**Commits/PRs:** Conventional Commits; PR template asks for contract changes, security impact, test evidence, screenshots (demo mode, light+dark).
**Docs:** any user-visible change updates the relevant doc in the same PR; ADR for decisions that are expensive to reverse (template in [adr/README](adr/README.md)).
**Dependencies:** [ADR-0013](adr/0013-dependency-policy.md).

## 6. Adding a feature (the vertical-slice checklist)

1. Update `api/openapi.yaml`, `docs/DATA-MODEL.md`, schemas; add a migration.
2. Add provider interface + **mock** implementation + fixtures.
3. Implement repository, service, handlers; authorisation metadata on routes; audit events; privileged method (if any) with hostile-input tests.
4. `make gen`; build UI against `--demo`; i18n keys (`en`, `it`); a11y + states (loading/empty/error/capability-missing).
5. Real provider; capture fixtures; VM integration test.
6. Docs + changelog entry; self-review with [CODE-REVIEW](engineering/CODE-REVIEW.md).

## 7. Debugging

`yubeosd --log-level=debug`; `GET /system/info` shows detected capabilities/provider choices; `yubeosctl doctor` checks permissions, socket, units, DB, audit chain, certificate expiry; `journalctl -u yubeosd -u yubeos-priv -f`.

## 8. Repo hygiene

Branch protection on `main`; required checks: `lint`, `test`, `build`, `openapi`, `schemas`, `codeql`; signed tags `vX.Y.Z`; releases created by CI only (artifacts: tarball, `.deb`, SBOM, checksums, cosign signatures).
