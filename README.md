# Yubeos

> A web-managed homelab / NAS / home-server platform for Linux — a TrueNAS-style control plane that runs on top of a stock Ubuntu Server or Debian install.

**Status:** `pre-alpha` — Phase 0 (foundations) implemented; sections are placeholders until their phase. See [ROADMAP](docs/ROADMAP.md).

---

## What is Yubeos?

Yubeos turns a plain Linux machine into a managed home server. A single static binary serves a web GUI from which you monitor hardware, manage disks and ZFS pools, browse and transfer files, open a real terminal, install containerised add-ons, follow trending open-source repositories and research papers, and restyle the interface — without leaving the browser.

It is a **platform layer**, not a new kernel or distribution. Linux, ZFS, Samba and Docker stay the source of truth; Yubeos orchestrates them through a small, audited privilege boundary.

| Section | What it does | Spec |
|---|---|---|
| **Dashboard** | Live CPU (per socket / per core), RAM (+ ZFS ARC), network, GPU, disk widgets | [dashboard](docs/modules/dashboard.md) |
| **File Explorer** | Tree/table browser, upload, download, basic file operations | [files](docs/modules/files.md) |
| **Disk Manager** | Disk inventory, SMART health, aliases/tags, ZFS pools, RAID layouts, SMB/NFS sharing | [storage](docs/modules/storage.md) |
| **Terminal** | Multi-tab PTY shell in the browser, per authenticated Linux user | [terminal](docs/modules/terminal.md) |
| **Add-ons** | Compose-based app catalogue with lifecycle management and embedded UIs | [addons](docs/modules/addons.md) |
| **Repo-Papers** | Ranked GitHub repositories and papers by time window and topic | [repo-papers](docs/modules/repo-papers.md) |
| **Appearance** | Theme engine: colours, fonts, density, layout, presets, import/export | [appearance](docs/modules/appearance.md) |

## Design goals

1. **Professional and boring where it matters.** Predictable, typed, tested, documented.
2. **Least privilege.** The network-facing process is unprivileged; root actions go through a minimal allow-listed helper.
3. **Linux stays the source of truth.** Yubeos never hides state that `zpool`, `smb.conf` or `docker` would show you.
4. **Contract first.** OpenAPI + JSON Schemas are written before the code that implements them.
5. **Develop anywhere.** A `--demo` mode with deterministic mock hardware lets you build the UI on a laptop (including Windows + WSL2).
6. **Small footprint.** One Go binary (UI embedded), SQLite, no external services.

## Documentation map

- **Start here:** [Architecture](docs/ARCHITECTURE.md) → [Security](docs/SECURITY.md) → [Roadmap](docs/ROADMAP.md)
- **Contracts:** [API](docs/API.md) · [Data model](docs/DATA-MODEL.md) · [`schemas/`](schemas/)
- **Front end:** [UI design](docs/UI-DESIGN.md) · [Appearance module](docs/modules/appearance.md)
- **Working on it:** [Development](docs/DEVELOPMENT.md) · [Testing](docs/TESTING.md) · [Deployment](docs/DEPLOYMENT.md) · [Contributing](CONTRIBUTING.md)
- **Engineering policy:** [Code review](docs/engineering/CODE-REVIEW.md) · [Tech debt](docs/engineering/TECH-DEBT.md)
- **Decisions:** [ADR index](docs/adr/README.md) · [Open questions](docs/OPEN-QUESTIONS.md)

## Target platform (v1.0)

| | |
|---|---|
| OS | Ubuntu Server 24.04 LTS (primary), Debian 13 (secondary) |
| Arch | x86_64 (arm64 post-1.0) |
| Storage | OpenZFS (primary), single-disk ext4/XFS volumes |
| Containers | Docker Engine + Compose v2 |
| Browsers | Current Chrome / Edge / Firefox / Safari |

## Quick start (planned, Phase 8)

```bash
curl -fsSL https://github.com/OWNER/yubeos/releases/latest/download/install.sh | sudo bash
# open https://<server>/  and sign in with a Linux account in the `yubeos-admin` group
```

## Try it now (Phase 0 — demo mode)

```bash
git clone https://github.com/OWNER/yubeos && cd yubeos
make deps && make demo     # https://localhost:8443 (self-signed certificate, mock hardware, NO authentication)
make test lint             # Go (race) + web tests, vet, tsc, eslint, schema checks
```

Needs Go ≥ 1.22 and Node ≥ 22. Only the shell, `/healthz` and `/api/v1/system/info` exist so far; each section shows the roadmap phase that delivers it. See [Development](docs/DEVELOPMENT.md).

## License

Apache-2.0 (proposed — see [OPEN-QUESTIONS](docs/OPEN-QUESTIONS.md) Q2). `OWNER` placeholders in docs are replaced at repository creation.
