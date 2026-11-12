# Deployment and Operations

## 1. Distribution model ([ADR-0009](adr/0009-distribution.md))

Yubeos is a **layer** installed on a stock **Ubuntu Server 24.04 LTS** (or Debian 13) via an apt repository / `.deb`. An appliance ISO (`live-build`) is a post-1.0 option. Rationale: smaller attack surface to maintain, users keep standard OS updates, drivers (NVIDIA, ZFS) come from the distro.

## 2. What the package installs

| Path | Content |
|---|---|
| `/usr/bin/yubeosd`, `/usr/libexec/yubeos/yubeos-priv`, `/usr/libexec/yubeos/yubeos-fsworker`, `/usr/bin/yubeosctl` | binaries (static where possible; priv dynamically linked to libpam) |
| `/lib/systemd/system/{yubeosd,yubeos-priv}.service` (+ `yubeos-priv.socket` optional) | units |
| `/etc/yubeos/config.yaml` | config (conffile; never overwritten without prompt) |
| `/etc/pam.d/yubeos` | PAM service |
| `/var/lib/yubeos/` | DB, secret key, fonts, backups (`0750 yubeos`) |
| `/run/yubeos/priv.sock` | RPC socket (`0660 root:yubeos`) |
| `/etc/samba/yubeos.conf`, `/etc/exports.d/yubeos.exports` | managed share fragments (created on first share) |
| `/usr/share/yubeos/` | presets, catalogue public key, docs |

Packages `Depends:` on `zfsutils-linux`, `smartmontools`, `samba`, `nfs-kernel-server`, `docker-ce`/`docker.io` + `docker-compose-plugin` are `Recommends:` so a minimal install is possible; the UI reflects missing capabilities.

## 3. Install, first run, upgrade, rollback

```bash
curl -fsSL https://github.com/OWNER/yubeos/releases/latest/download/install.sh | sudo bash   # verifies checksum + cosign signature
# or: add apt repo (key pinned), apt install yubeos
sudo yubeosctl doctor
sudo groupadd -f yubeos-admin && sudo usermod -aG yubeos-admin <your-user>
```

- **First run:** generates TLS cert + secret key, creates DB, prints URL and certificate fingerprint. No default password exists: login uses Linux accounts in `yubeos-admin`.
- **Upgrade:** `apt upgrade`; `preinst` backs up DB; migrations run on start; if start fails, systemd keeps the previous binary available (`yubeos-previous`) and `yubeosctl rollback` restores DB backup + binary.
- **Uninstall:** `apt remove` leaves data; `apt purge` removes `/var/lib/yubeos` after confirmation. Pools, shares data and containers are **never** removed.

## 4. Network and ports

| Port | Service | Default |
|---|---|---|
| 443/tcp | Yubeos HTTPS | on (configurable; `8443` recommended if another reverse proxy owns 443) |
| 80/tcp | redirect to HTTPS | on |
| 445/139, 2049 | SMB / NFS (managed) | only when shares exist |
| 20000–29999 | add-on host ports | allocated per add-on, LAN only by default |

Exposure guidance: keep on LAN/VPN (Tailscale/WireGuard); do not port-forward the UI to the internet. Firewall: installer offers to add `ufw` rules for LAN subnets only.

## 5. Operations

- **Logs:** journald (`yubeosd`, `yubeos-priv`, `yubeos-audit`).
- **Backup:** config + DB are small; `yubeosctl export-config > backup.tar.zst` (no secrets unless `--with-secrets`); ZFS data is protected by snapshots/replication, not by Yubeos.
- **Monitoring:** `/healthz`, optional `/metrics` (Prometheus).
- **Certificates:** self-signed renewed automatically before expiry; BYO/ACME paths in config.
- **Time:** relies on system NTP; clock jumps invalidate nothing but are logged.

## 6. Release artifacts

Tarballs (`linux-amd64`), `.deb`, apt repo metadata, `install.sh`, `SHA256SUMS`, cosign signatures, CycloneDX SBOM, release notes from CHANGELOG. Built only by GitHub Actions from signed tags.
