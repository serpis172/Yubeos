# Deployment and Operations

## 1. Distribution model ([ADR-0009](adr/0009-distribution.md))

Yubeos is a **layer** installed on a stock **Ubuntu Server 24.04 LTS** (or Debian 13) via an apt repository / `.deb`. An appliance ISO (`live-build`) is a post-1.0 option. Rationale: smaller attack surface to maintain, users keep standard OS updates, drivers (NVIDIA, ZFS) come from the distro.

## 2. What the package installs

| Path | Content |
|---|---|
| `/usr/bin/yubeosd`, `/usr/libexec/yubeos/yubeos-priv`, `/usr/libexec/yubeos/yubeos-fsworker`, `/usr/bin/yubeosctl` | binaries (static where possible; priv dynamically linked to libpam) |
| `/lib/systemd/system/{yubeosd,yubeos-priv}.service` (+ `yubeos-priv.socket` optional) | units |
| `/etc/yubeos/config.yaml` | config (conffile; never overwritten without prompt) |
| `/var/lib/yubeos-priv/` | credential store, pepper and secret keys (root, `0700`) — **never** readable by `yubeosd` |
| `/etc/pam.d/yubeos` | PAM service (only when built with the `pam` tag, LAN mode) |
| `/etc/fail2ban/filter.d/yubeos.conf`, `/etc/fail2ban/jail.d/yubeos.local` | fail2ban filter/jail on `yubeos-auth` log lines (installed if fail2ban present) |
| `/var/lib/yubeos/` | DB, secret key, fonts, backups (`0750 yubeos`) |
| `/run/yubeos/priv.sock` | RPC socket (`0660 root:yubeos`) |
| `/etc/samba/yubeos.conf`, `/etc/exports.d/yubeos.exports` | managed share fragments (created on first share) |
| `/usr/share/yubeos/` | presets, catalogue public key, docs |

Packages `Depends:` on `zfsutils-linux`, `smartmontools`, `samba`, `nfs-kernel-server`, `docker-ce`/`docker.io` + `docker-compose-plugin` are `Recommends:` so a minimal install is possible; the UI reflects missing capabilities.

## 3. Install, first run, upgrade, rollback

```bash
curl -fsSL https://github.com/serpis172/Yubeos/releases/latest/download/install.sh | sudo bash   # verifies checksum + cosign signature
# or: add apt repo (key pinned), apt install yubeos
sudo yubeosctl doctor
sudo groupadd -f yubeos-admin && sudo usermod -aG yubeos-admin <your-user>
sudo yubeosctl user add <your-user> --admin       # prints the one-time setup link
```

- **First run:** generates TLS cert + keys, creates DBs, prints URL and certificate fingerprint. **No default password exists:** create the first admin with `sudo yubeosctl user add <linux-user> --admin`, which prints a one-time setup link (1 h, local origin only) where you choose a password and enrol a passkey/TOTP ([AUTHENTICATION §9](AUTHENTICATION.md#9-bootstrap-recovery-and-administration)).
- **Upgrade:** `apt upgrade`; `preinst` backs up DB; migrations run on start; if start fails, systemd keeps the previous binary available (`yubeos-previous`) and `yubeosctl rollback` restores DB backup + binary.
- **Uninstall:** `apt remove` leaves data; `apt purge` removes `/var/lib/yubeos` after confirmation. Pools, shares data and containers are **never** removed.

## 4. Network and ports

| Port | Service | Default |
|---|---|---|
| 443/tcp | Yubeos HTTPS | on (configurable; `8443` recommended if another reverse proxy owns 443). For remote access see [REMOTE-ACCESS](REMOTE-ACCESS.md) |
| 80/tcp | redirect to HTTPS | on |
| 445/139, 2049 | SMB / NFS (managed) | only when shares exist |
| 20000–29999 | add-on host ports | allocated per add-on, LAN only by default |

Exposure guidance: the recommended way to reach Yubeos from outside is a private overlay (Tailscale/WireGuard); tunnel and direct options are supported with enforced safety rules — read [REMOTE-ACCESS](REMOTE-ACCESS.md) before forwarding any port. Firewall: installer offers `ufw` rules for LAN subnets only and never opens a port to the internet by itself.

## 5. Operations

- **Logs:** journald (`yubeosd`, `yubeos-priv`, `yubeos-audit`).
- **Backup:** config + DB are small; `yubeosctl export-config > backup.tar.zst` (no secrets unless `--with-secrets`); ZFS data is protected by snapshots/replication, not by Yubeos.
- **Monitoring:** `/healthz`, optional `/metrics` (Prometheus).
- **Certificates:** self-signed renewed automatically before expiry; BYO/ACME paths in config.
- **Time:** relies on system NTP; clock jumps invalidate nothing but are logged.

## 6. Release artifacts

Tarballs (`linux-amd64`), `.deb`, apt repo metadata, `install.sh`, `SHA256SUMS`, cosign signatures, CycloneDX SBOM, release notes from CHANGELOG. Built only by GitHub Actions from signed tags.
