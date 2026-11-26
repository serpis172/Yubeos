# 7. Troubleshooting and FAQ

## Quick checks

```bash
curl -sk https://localhost:8443/healthz               # {"ok":true} means the daemon is up
curl -sk https://localhost:8443/api/v1/system/info    # 401 outside demo mode = not signed in
make test lint                                        # verify your checkout
```

## Problems

| Symptom | Cause and fix |
|---|---|
| Browser warns about the certificate | Expected with the auto-generated self-signed certificate. Accept it for local use; use `tls.mode: files` or `acme` for a trusted one. |
| Top bar shows **not signed in** | The API returned 401: authentication is default-deny outside `--demo`. Sign in, or use `--demo` for development. |
| No **Sign in with a passkey** button | Passkeys need HTTPS (or `localhost`) and a browser that supports WebAuthn. Use password + authenticator code instead. |
| “Set one up from a computer on the local network first” | Your policy needs a second factor and you have none: enrol it from the LAN (or an overlay address treated as local), then sign in remotely. |
| “Confirm it is you” keeps appearing | The step-up window lasts 5 minutes and is never granted by a trusted device. Confirm with a passkey, authenticator code or recovery code. |
| “Keep at least one second factor” | Policy requires a second factor for your account, so the last one cannot be removed. Add another first. |
| Dashboard pill says **Reconnecting…** or **Updating every 2 s** | The WebSocket cannot connect. Behind a reverse proxy make sure it forwards `Upgrade`/`Connection` headers and the original `Host` ([chapter 4](04-remote-access.md#42-examples)); the browser must reach Yubeos at the same address the page was loaded from. Polling keeps the Dashboard working meanwhile. |
| A Dashboard card is dimmed or says a provider is unavailable | Its data source stopped. Read the reason on the page (e.g. `nvidia-smi` crashed, RAPL needs the privileged helper) and the daemon log; the card recovers by itself. |
| No GPU card | Only NVIDIA GPUs with `nvidia-smi` installed are supported (`providers.gpu: none` hides it on purpose). |
| Disk cards show no temperature | Load the kernel `drivetemp` module for SATA drives (`sudo modprobe drivetemp`); NVMe drives report it natively. |
| CPU power is missing | RAPL counters are root-only: the `yubeos-priv` helper must run, and the CPU must expose `/sys/class/powercap/intel-rapl:*`. |
| Passkey marked **blocked: possible clone** | Its signature counter went backwards. Remove it and enrol a new one; check the audit log. |
| Every remote request returns 503 | Lockdown is on: `sudo yubeosctl lockdown off` (or Settings → Security from a local address). |
| Lost authenticator and recovery codes | `sudo yubeosctl user reset <user> --2fa` on the server, then re-enrol from the LAN. |
| ACME certificate not issued | Ports 443 and 80 must reach `yubeosd` from the internet, `acmeDomains` must resolve to this host, and `allowedHosts` must include them. Check the log for `acme` messages. |
| `-no-tls` is rejected | Plain HTTP is allowed only with `--demo` (for the Vite dev proxy). |
| `yubeosd` refuses to start with `exposure=remote …` | A remote-mode rule failed. Read the message: it names the missing item (`allowedHosts`, trusted certificate or `behindProxy`, `auth.mode=native`, `require2fa=all`, `allowRoot=false`). See [chapter 4](04-remote-access.md#43-the-safety-net). |
| `unknown field` when loading the config | Keys are validated strictly; fix the typo or check [`config.schema.json`](../../schemas/config.schema.json). |
| `make demo` fails on `npm ci` | Needs Node ≥ 22. Check `node -v`. |
| Port 8443 busy | `make demo DEMO_PORT=9443` or `yubeosd --demo --listen :9443`. |
| UI shows stale API types after editing the contract | Run `make gen`. |
| Theme or language resets on another browser | They are stored per browser for now ([TD-028](../engineering/TECH-DEBT.md)). Export your custom theme from Appearance and import it elsewhere. |

## FAQ

**Can I run it on Windows?** Yes for development: use WSL2 and keep the repository inside the Linux filesystem. Storage, SMART and GPU screens use mocks there.

**Does Yubeos replace ZFS, Samba or Docker?** No. It orchestrates them; the system tools stay the source of truth.

**Is it safe to expose to the internet?** It can be, with the enforced remote rules, but a private overlay (Tailscale, WireGuard) is the recommended default. Read [Remote access](04-remote-access.md).

**I lost my phone with the authenticator.** Use a recovery code, or run `sudo yubeosctl user reset <user> --2fa` on the server, then enrol again from the LAN.

**Where do I report a bug or contribute?** See [CONTRIBUTING](../../CONTRIBUTING.md); the working method is described in [Development](../DEVELOPMENT.md).
