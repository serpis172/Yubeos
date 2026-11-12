# Open Questions

Nothing here blocks Phase 0: each question has a **default** already used by the documents. Answers may change defaults; update the referenced docs and ADRs when resolved.

| # | Question | Default used | Affects |
|---|---|---|---|
| Q1 | Project name/branding: sketches show "yubeos"; confirm spelling, logo, GitHub owner/org and repo name (docs use `OWNER`). | `Yubeos`, repo `yubeos` | README, module path, packages |
| Q2 | License | Apache-2.0 (max adoption; alternative GPL-3.0/AGPL-3.0 to force open derivatives) | LICENSE, CONTRIBUTING |
| Q3 | Documentation language | English docs + UI i18n `en`/`it`; `README.it.md` can be added | docs |
| Q4 | Base OS priority | Ubuntu 24.04 primary, Debian 13 secondary, x86_64 | packaging, CI |
| Q5 | Storage scope | ZFS-first (as in the source manual), ext4/XFS single disks; no mdadm/LVM/btrfs/Unraid-style pooling in 1.0 | storage module |
| Q6 | Users model | Linux accounts; groups `yubeos-admin` / `yubeos-user`; normal users get Files, Terminal (optional), Add-ons browse, Repo-Papers | auth, RBAC |
| Q7 | Repo-Papers placement | Built-in first-party module (not a container add-on); sources: GitHub, arXiv, Semantic Scholar, Hugging Face, OpenAlex | repo-papers |
| Q8 | Repo-Papers topics/interests and GitHub token availability | Built-in topics list; token optional but recommended | repo-papers |
| Q9 | GPU vendors in scope | NVIDIA in 1.0; AMD 1.1; Intel 1.2 | dashboard |
| Q10 | Remote access model | LAN/VPN only; self-signed TLS by default; ACME later | security, deployment |
| Q11 | Terminal access for non-admin users | Disabled by default (`terminal.allowUsers: false`) | terminal |
| Q12 | Default look | TrueNAS-like "Yubeos Blue" dark; the original manual's palette is the "Yubeos Midnight" preset | appearance |
| Q13 | Disk sketch details | Read "546GB / 312GB" as sample values; "Pool/RAID/Sharing — local" interpreted as local pool management plus SMB/NFS sharing on the LAN | storage |
| Q14 | Sidebar sketch | "ADD-ONS" = catalogue/manager; installed add-ons listed below as separate entries; "REPO-PAPERS" = built-in section | UI |
| Q15 | Appliance ISO | Post-1.0 | roadmap |
