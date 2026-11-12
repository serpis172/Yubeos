# ADR-0006: OpenZFS as the primary storage engine behind a Backend interface

**Status:** Accepted · **Date:** 2026-11-11

## Context
Target users want pools, RAID-Z, snapshots, scrubs and checksumming (TrueNAS-like). Ubuntu 24.04 ships OpenZFS 2.2.x without JSON output for `zpool status`; newer releases add `-j`.

## Decision
ZFS is the first-class backend; plain ext4/XFS volumes second. Access through `storage.Backend`. Parsing: scriptable `-H -p` for list/get; fixture-tested text parser for `zpool status` on < 2.3; `-j` when capability detected. Devices addressed by `/dev/disk/by-id`. mdadm/LVM/btrfs deferred.

## Consequences
Best data integrity and features; ZFS licensing/kernel considerations are delegated to the distro (Ubuntu ships it). Parser maintenance burden across versions (mitigated by golden tests, spike S-4).

## Alternatives considered
mdadm+LVM+ext4: simpler but fewer features. btrfs: RAID5/6 caveats. Unraid-style pooling (mergerfs+SnapRAID): different semantics; may become an alternative backend.
