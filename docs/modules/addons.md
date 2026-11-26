# Module: Add-ons

Routes `/addons` (catalogue + manager), `/addons/<id>` (embedded UI) · Roles: browse = user/admin, mutate = admin · Backend: `internal/addons` · API: [`/addons/*`](../API.md#add-ons)

Sidebar behaviour from the sketch: **ADD-ONS** (manager) and then each **installed add-on** appears as its own sidebar entry (ADD-ON 1, ADD-ON 2 …) that opens its UI inside the shell.

## 1. Concept

An add-on is a **Docker Compose application with a Yubeos manifest** (`addon.yaml`). Yubeos adds what Compose lacks: catalogue, schema-validated configuration form, dataset-backed storage mapping, port-conflict detection, health, lifecycle jobs, sidebar integration and an authenticated reverse proxy. First-party features may ship as add-ons (e.g. Repo-Papers is *bundled and pre-installed* but built-in, see its spec).

Runtime types: `compose` (v1). `native` (systemd unit) is reserved in the schema, not implemented.

## 2. Manifest (`addon.yaml`) — validated by [`schemas/addon.schema.json`](../../schemas/addon.schema.json)

```yaml
apiVersion: yubeos.dev/v1
kind: Addon
metadata:
  id: jellyfin                    # [a-z0-9-], unique per catalogue
  name: Jellyfin
  version: 10.10.3-1              # add-on package version (not necessarily app version)
  description: Media server
  icon: icon.svg
  categories: [media]
  author: Example
  license: GPL-2.0
  homepage: https://jellyfin.org
spec:
  runtime: compose
  compose: compose.yaml           # relative file in the add-on directory
  ui: { service: jellyfin, port: 8096, path: "/", embed: link, sidebar: true }   # embed: iframe (isolated subdomain only) | link | none
  storage:
    - { name: config, mount: /config, kind: appdata, required: true }
    - { name: media,  mount: /media,  kind: dataset, access: ro, required: false }
  network: { expose: lan, ports: [ { container: 8096, host: auto, proto: tcp } ] }
  resources: { gpu: optional, memoryLimit: 4g }
  permissions: { privileged: false, dockerSocket: false, hostNetwork: false, devices: [] }
  config:                         # rendered as a form; values injected as env/compose variables
    - { key: TZ, type: string, default: "Europe/Rome", label: Timezone }
    - { key: PUBLISHED_URL, type: url, required: false }
  healthcheck: { service: jellyfin, path: /health, intervalSeconds: 30 }
```

## 3. Lifecycle

```
available ─install─▶ installing ─▶ starting ─▶ running ⇄ stopped
                          │            │          │
                          ▼            ▼          ▼
                       failed       unhealthy   updating ─▶ running | rollback
running/stopped ─remove─▶ removing ─▶ removed (data kept unless "delete data" chosen + step-up)
```

All transitions are **jobs**. Compose project name `yubeos-<id>`; all objects labelled `dev.yubeos.addon=<id>`, `dev.yubeos.managed=true`. Yubeos only manages objects carrying its labels.

**Install flow:** fetch manifest → schema validate → capability checks (Docker present, GPU runtime if requested) → permission review dialog (admin approves privileged/hostNetwork/devices) → storage resolution (create `appdata` dataset/dir under `/mnt/<pool>/yubeos/addons/<id>/`; bind selected datasets) → port allocation (range 20000–29999 by default; conflict check against `ss -ltn` and other add-ons) → render override file `compose.yubeos.yaml` (labels, limits, ports, bind mounts) → `compose pull` (progress) → `compose up -d` → wait healthy (timeout 5 min) → register sidebar entry.

**Update:** pull new digest → recreate → health check → on failure automatic rollback to previous image digest (kept until success). **Backup of appdata** snapshot taken first when appdata is a ZFS dataset.

## 4. Catalogue

- A catalogue is a **git repository or HTTPS tarball** with `index.json` (generated in CI) + `addons/<id>/{addon.yaml,compose.yaml,icon,README.md}`.
- `official` catalogue (`serpis172/Yubeos-addons`) is signed (minisign/cosign); verification on refresh; unsigned/foreign catalogues require explicit admin opt-in and are badged *Community*.
- Refresh daily + manual; ETag caching; stored in SQLite `addon_catalog_entries`.
- First-party manifests **pin images by digest**; community ones are flagged if they use mutable tags.

## 5. UI access and isolation ([ADR-0018](../adr/0018-addon-ui-isolation.md))

Add-on code is untrusted. Its UI is **never served from Yubeos' origin or hostname**:

| Mode | URL | Requirements | Notes |
|---|---|---|---|
| **Isolated subdomain** (preferred; required when `exposure: remote`) | `https://<id>.<addons.baseDomain>` | wildcard DNS + wildcard certificate; `addons.baseDomain` set | `yubeosd` proxies to `127.0.0.1:<hostPort>` after a **single-use, 60 s, add-on-scoped ticket** handoff from the Yubeos session; the add-on hostname gets its own cookie; Yubeos' host-only `__Host-` cookie is never sent to it; WebSocket upgrade supported |
| **Direct link** (LAN fallback) | `http(s)://<other-hostname>:<hostPort>` | the add-on must be reachable under a hostname **different from the one used for Yubeos** (e.g. the server IP) | Cookies are not isolated by port, so Yubeos refuses to build a same-hostname link and shows the alternate address |
| None | — | — | headless add-ons |

Rules for any proxied response: Yubeos cookies/headers are never forwarded upstream; `X-Forwarded-*` added; CSP for the add-on hostname is its own (`frame-ancestors` only the Yubeos origin when `embed: iframe`); an `iframe` embed uses `sandbox="allow-scripts allow-forms allow-popups"` plus `allow-same-origin` **only** because the content lives on a different origin. A test asserts that nothing under Yubeos' hostname serves add-on-controlled HTML.

## 6. Security

Privileged operations go through `yubeos-priv` (`compose.*`); `yubeosd` never reaches `docker.sock`. Defaults per container: `no-new-privileges`, `cap_drop: [ALL]` unless the manifest lists required caps, read-only root FS when the manifest says `readOnlyRootfs: true`, memory/CPU limits applied, no `--net=host`, no `--privileged`, no `docker.sock`. Anything beyond defaults is shown to the admin in the install dialog and recorded in the audit log.

## 6b. Implemented so far: list and open

The read-only slice ships before the lifecycle (install/update/rollback, ticketed proxy) of Phase 7:

- `yubeos-priv` method `compose.list` (read-only) runs `docker ps` / `docker inspect` for containers labelled `dev.yubeos.managed=true` and groups them by `dev.yubeos.addon`. The daemon never touches `docker.sock`.
- `GET /api/v1/addons` adds a safe URL per ADR-0018: `https://<id>.<addons.baseDomain>` when a base domain is configured, otherwise `http://<server LAN IP>:<published port>` — and only when that host differs from the one used to reach Yubeos (`urlBlocked: "same-host"` otherwise). `embed: none` gets no link.
- The UI opens the URL with `target="_blank" rel="noopener noreferrer"`; no add-on HTML is served from Yubeos' origin. If Docker or the helper is unavailable the list is empty with `unavailable: true`.
- `--demo` returns three mock add-ons.

## 7. Acceptance criteria (Phase 7)

1. Install/stop/start/update/rollback/remove of two reference add-ons (one with UI proxy, one `link`) in VM tests; failure injection (bad image, port taken, unhealthy) leaves the system clean.
2. Removing an add-on never deletes data without the explicit step-up option.
3. Manifest fuzz/negative tests: path traversal in `compose:`, oversized fields, unknown permissions → rejected with precise errors.
4. Dashboard and Files remain responsive during an image pull.
