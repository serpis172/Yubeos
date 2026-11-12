# Module: Disk Manager (storage)

Route `/disks` · Roles: admin (read-only subset for user: none in v1) · Backend: `internal/storage`, `internal/shares` · API: [`/storage/*`, `/shares/*`](../API.md#storage)

From the sketch: left list **DISKS** (Disk 1 ›); detail header with an editable **name** (✎) and a hover card; table **Type · Capacity · Occupied · Pool · Tag** (e.g. `SSD · 546 GB · 312 GB · — · MAIN OS`); a **Disk health / utilisation and status** panel; and a lower area **Pool management, RAID & sharing (local)**.

## 1. Information architecture

```
Disk Manager
├── Disks            (inventory, alias, tags, health, utilisation)      — Phase 5a (read-only), 5b (alias/tags, SMART tests)
├── Pools            (ZFS pools, vdev topology/RAID level, scrub, status) — Phase 5c (read), 5d (write)
├── Datasets         (tree, quota, compression, snapshots)               — Phase 5e
└── Sharing          (SMB / NFS shares bound to datasets or paths)       — Phase 6
```

## 2. Disks

**Inventory** (`lsblk -J -b -O` + `/dev/disk/by-id` + `smartctl -j -i`): id, model, serial, firmware, WWN, transport (SATA/SAS/NVMe/USB/virtio), **type** (`HDD` rotational=1, `SSD`, `NVMe`), capacity, partitions, mountpoints, pool membership, `isBoot` (backs `/` or `/boot`), `isRemovable`.

**Occupied:** for pool members: allocated share of the vdev; for plain volumes: `statfs` of mount; for unused disks: `0`. Never computed by summing files.

**Alias:** free-text name (1–40 chars) stored in `disk_meta` keyed by stable ID; shown everywhere (dashboard, pools). *Hover card* (sketch note "appears on hover: Disk 1"): alias, model, serial, id, size, temp, SMART verdict.

**Tags:** user tags (free text, colour) + **system tags** auto-derived and non-removable: `MAIN OS` (boot disk), `POOL:<name>`, `SPARE`, `UNUSED`, `FAILED`. A disk can carry many tags.

**Health:** SMART overall verdict (`PASSED/FAILED`), key attributes (reallocated, pending, uncorrectable, CRC errors, power-on hours, wear % for SSD, media errors for NVMe), temperature history, self-test log; actions: start short/long test (job), view results. Verdict logic: `FAILED` if SMART says so or any critical attribute > 0 growing; `WARN` on thresholds in config.

**Utilisation & status:** live read/write MB/s, IOPS, busy %, queue depth (from `/proc/diskstats`), status chip `ONLINE | DEGRADED | FAULTED | SPARE | UNASSIGNED`.

## 3. Pools and RAID (ZFS backend — [ADR-0006](../adr/0006-zfs-primary.md))

| User-facing layout | ZFS vdev | Min disks | Fault tolerance | Notes |
|---|---|---|---|---|
| Stripe (no redundancy) | plain vdevs | 1 | none | warn loudly |
| Mirror (RAID1) | `mirror` (2–3-way) | 2 | n−1 | best IOPS, simplest resilver |
| RAID-Z1 (RAID5-like) | `raidz1` | 3 | 1 disk | discourage with ≥ 8 TB disks (warning) |
| RAID-Z2 (RAID6-like) | `raidz2` | 4 | 2 disks | recommended default for ≥ 4 disks |
| RAID-Z3 | `raidz3` | 5 | 3 disks | |
| Stripe of mirrors (RAID10) | multiple `mirror` | 4 | 1 per mirror | |

**Create pool wizard:** pick disks (only `UNUSED`, never boot/in-use) → choose layout → name (regex `^[a-zA-Z][a-zA-Z0-9_.:-]{0,63}$`, reserved names rejected) → options: `ashift` (auto from sector size, default 12), compression `lz4` (default), `atime=off`, encryption (native, v1.1), optional log/cache/special/spare vdevs (advanced, v1.1) → **dry-run** (`zpool create -n`) preview of the resulting topology → step-up confirmation → job.

**Day-2:** status (health, scan state, errors per vdev), **scrub** start/stop + schedule (default monthly), replace disk, add vdev (with irreversibility warning), export/import (import lists candidates only; never auto-import), expand, clear errors. **Destroy pool** = destructive class + typed name + re-auth.

**Datasets:** create/rename/destroy, mountpoint, quota/reservation, compression, recordsize presets (general, media, db, vm), snapshots (manual + policies: hourly/daily/weekly retention), rollback (destructive), clone (v1.2), replication (post-1.0).

**Non-ZFS volumes:** single disk formatted ext4 (default) or XFS: wipe → partition (GPT) → `mkfs` → mount by UUID in a Yubeos-owned `/etc/fstab.d`-style fragment (`# managed by yubeos` block, `nofail`) → surfaced to Files/Sharing. Same safety rules (never touch boot/in-use disks). *Out of scope v1:* mdadm RAID, LVM, btrfs, Unraid-style parity pooling (Backend interface leaves the door open).

## 4. Backend interface

```go
type Backend interface {
    Capabilities(ctx) Caps                       // versions, features, JSON support
    Disks(ctx) ([]Disk, error)
    Pools(ctx) ([]Pool, error)
    PlanPool(ctx, PoolSpec) (Plan, error)        // dry run, returns topology + warnings
    CreatePool(ctx, PoolSpec, Approval) (JobID, error)
    Scrub(ctx, pool string, op ScrubOp) error
    Datasets(ctx, pool string) ([]Dataset, error)
    // … snapshots, replace, import/export
}
```

Implementations: `zfs` (real), `plain` (ext4/XFS), `mock` (deterministic fixtures incl. degraded pool, failing disk). Parsing strategy and fixtures are described in [ARCHITECTURE §14](../ARCHITECTURE.md#14-compatibility-and-version-probing); every parser has golden-file tests from real command output captured on OpenZFS 2.2 and 2.3.

## 5. Sharing ("local" sharing in the sketch)

- **SMB (Samba)** — primary. A share = `{name, path (dataset or directory under managed roots), comment, readOnly, guestOk, validUsers[], validGroups[], browseable, timeMachine, recycleBin, hostsAllow[]}`. Yubeos renders `/etc/samba/yubeos.conf` (included once from `smb.conf` via an `include =` line added by the installer), validates with `testparm -s`, then `smbcontrol all reload-config`. Samba users map to Linux users (`smbpasswd` handled through helper method `samba.setpassword`).
- **NFS** — `/etc/exports.d/yubeos.exports`, `exportfs -ra`; v4 only by default; per-network ACLs.
- **ZFS properties** `sharesmb`/`sharenfs` are *not* used (Samba/NFS config stays explicit and portable).
- Share path must be inside a configured root; ownership/permissions preset helper (user/group, `0770`, ACL mode) — **applies to new datasets only, never recursively rewrites existing data without a confirm job**.
- Service control: enable/disable/restart `smbd`, `nmbd` (optional), `nfs-server`; live connection list (`smbstatus -j`).
- Drift detection: if the include file is modified externally, UI shows drift and offers *re-apply* or *adopt*.

## 6. Safety rules (enforced server-side)

1. Boot/OS disk and any disk with a mounted filesystem or active pool membership are **never** selectable for wipe/format/add.
2. Every create/destroy/format first runs a **plan** step; the UI shows the plan and the API requires the returned `planId` (expires 5 min, bound to current inventory hash) to execute — prevents acting on stale views.
3. Destructive actions: typed name + password step-up ([SECURITY §4.3](../SECURITY.md#43-step-up-confirmation-for-destructive-operations)).
4. No automatic pool import/export/destroy, ever.
5. All device references re-resolved from fresh inventory at execution time.

## 7. Acceptance criteria

- **5a:** with a real ZFS box and with the mock, Disks table shows correct type/capacity/occupied/pool/tag; hover card works; SMART panel matches `smartctl`.
- **5d:** create/scrub/replace/destroy exercised in VM tests with file-backed and virtual disks; a degraded mirror is displayed `DEGRADED` within one refresh; replace completes.
- **6:** a Windows client mounts an SMB share with the configured user and respects read-only; config survives `smbd` restart and reboot; external edit triggers drift.
