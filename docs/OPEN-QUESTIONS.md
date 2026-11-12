# Open Questions

Nothing here blocks Phase 0: each question has a **default** already used by the documents. Answers may change defaults; update the referenced docs and ADRs when resolved.

| # | Question | Default used | Affects |
|---|---|---|---|
| Q1 | ~~Name, GitHub owner/repo~~ **Resolved:** `Yubeos`, `github.com/serpis172/Yubeos`. Logo still open (text mark "Y" for now). | — | — |
| Q2 | ~~License~~ **Resolved:** Apache-2.0 | — | — |
| Q3 | Documentation language | English docs + UI i18n `en`/`it`; `README.it.md` can be added | docs |
| Q4 | Base OS priority | Ubuntu 24.04 primary, Debian 13 secondary, x86_64 | packaging, CI |
| Q5 | Storage scope | ZFS-first (as in the source manual), ext4/XFS single disks; no mdadm/LVM/btrfs/Unraid-style pooling in 1.0 | storage module |
| Q6 | Users model | Web accounts linked to Linux accounts; groups `yubeos-admin` / `yubeos-user`; **separate Yubeos credentials** (Argon2id) + passkeys/TOTP; normal users get Files, Terminal (optional), Add-ons browse, Repo-Papers | auth, RBAC |
| Q7 | Repo-Papers placement | Built-in first-party module (not a container add-on); sources: GitHub, arXiv, Semantic Scholar, Hugging Face, OpenAlex | repo-papers |
| Q8 | Repo-Papers topics/interests and GitHub token availability | Built-in topics list; token optional but recommended | repo-papers |
| Q9 | GPU vendors in scope | NVIDIA in 1.0; AMD 1.1; Intel 1.2 | dashboard |
| Q10 | ~~Remote access model~~ **Requirement received:** reachable from outside the LAN with robust auth, 2FA and remembered devices → designed in [REMOTE-ACCESS](REMOTE-ACCESS.md) / [AUTHENTICATION](AUTHENTICATION.md). **Still open:** which access option you will use (overlay VPN / tunnel / direct port-forward), whether you have a domain name, and whether your ISP gives a public IPv4 (CGNAT blocks direct mode). | — | security, deployment |
| Q11 | Terminal access for non-admin users | Disabled by default (`terminal.allowUsers: false`) | terminal |
| Q12 | Default look | TrueNAS-like "Yubeos Blue" dark; the original manual's palette is the "Yubeos Midnight" preset | appearance |
| Q13 | Disk sketch details | Read "546GB / 312GB" as sample values; "Pool/RAID/Sharing — local" interpreted as local pool management plus SMB/NFS sharing on the LAN | storage |
| Q14 | Sidebar sketch | "ADD-ONS" = catalogue/manager; installed add-ons listed below as separate entries; "REPO-PAPERS" = built-in section | UI |
| Q15 | Appliance ISO | Post-1.0 | roadmap |
| Q16 | Passkeys vs hostname | Passkeys are bound to the hostname you use; pick the final hostname (e.g. `nas.<yourdomain>`) before enrolling them | auth |
| Q17 | Trusted-device lifetime | 30 days (admins 14 days when remote); change with `auth.trustedDeviceDays` | auth |
| Q18 | Recovery policy | No email/SMS recovery; recovery codes or root on the host (`yubeosctl user reset`) | auth |
| Q19 | Notifications for new-device sign-ins | In-app + audit in 1.0; webhook (ntfy/Gotify) in 1.1 | auth |
