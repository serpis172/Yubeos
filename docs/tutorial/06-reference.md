# 6. Commands and configuration reference

## 6.1 `yubeosd` ✅

The daemon. Settings are layered, lowest to highest priority: **built-in defaults → config file → `YUBEOS_*` environment → command-line flags.** Unknown config keys are rejected.

| Flag | Meaning |
|---|---|
| `-config <path>` | YAML/JSON config file. Empty = defaults + environment. |
| `-demo` | Mock hardware, no authentication. Development only. |
| `-demo-seed <n>` | Seed for the deterministic demo data of the Dashboard (default `42`). |
| `-listen <addr>` | Override `server.listen`. |
| `-log-level <level>` | Override `logging.level`. |
| `-no-tls` | Plain HTTP. Allowed only together with `-demo`. |
| `-state-dir <dir>` | State directory (default `/var/lib/yubeos`; demo: the user cache directory). |
| `-version` | Print version and exit. |

```bash
yubeosd --demo --listen :9443                       # try it
YUBEOS_LOGGING_LEVEL=debug yubeosd --demo           # environment override
yubeosd -config /etc/yubeos/config.json             # real run
```

**Environment mapping:** `YUBEOS_` + the JSON path upper-cased and joined with `_`. Examples: `YUBEOS_SERVER_LISTEN`, `YUBEOS_SERVER_TLS_MODE`, `YUBEOS_AUTH_REQUIRE2FA`.

HTTP endpoints: `GET /healthz` (public), `GET /api/v1/system/info`, and the authentication and security endpoints below. Everything is described in the [API contract](../API.md) and [`api/openapi.yaml`](../../api/openapi.yaml).

| Endpoints (all under `/api/v1`) | Purpose |
|---|---|
| `POST /auth/login` → 200 or 202 | Password login; 202 means a second factor is needed (`challengeId`, `methods`) |
| `POST /auth/2fa/verify` | Finish login with an authenticator or recovery code (`trustDevice` optional) |
| `POST /auth/webauthn/login/begin` · `/finish` | Passkey login (alone, or as second factor) |
| `GET /auth/factors`, `POST /auth/totp/enroll` · `/confirm`, `DELETE /auth/totp` | Authenticator app |
| `POST /auth/webauthn/register/begin` · `/finish`, `DELETE /auth/webauthn/{id}` | Passkeys |
| `POST /auth/recovery-codes:regenerate` | New recovery codes (step-up) |
| `POST /auth/stepup/begin` · `/auth/stepup` | Open the 5-minute step-up window |
| `GET /auth/devices`, `DELETE /auth/devices/{id}`, `POST /auth/devices:revokeAll` | Sessions and trusted devices |
| `GET /security/remote-status`, `POST /security/lockdown` | Admin: readiness checklist and lockdown switch |
| `GET /metrics/catalog`, `GET /metrics/snapshot`, `GET /metrics/history?series=…&range=5m\|1h\|24h\|7d\|30d` | Dashboard data: series and devices, latest values, history |
| `GET /ws/metrics?series=…` (WebSocket) | Live frames `{t, v:{series:value}}`; first frame is a snapshot; `{dropped:n}` if you are too slow |
| `GET /me/preferences`, `PUT /me/preferences` | Per-user theme, language, units |

Errors are `application/problem+json`; the `type` ends with a stable code such as `invalid-second-factor`, `step-up-required`, `last-factor` or `lockdown`.

## 6.2 `make` targets ✅

| Target | Does |
|---|---|
| `make help` | List targets |
| `make deps` | `go mod download` + `npm ci` |
| `make gen` | Regenerate the TypeScript client from `api/openapi.yaml` |
| `make web` | Build the UI into `web/dist` (embedded in the binary) |
| `make build` | Static binary in `dist/yubeosd` |
| `make demo` | Build UI and run the demo on `https://localhost:8443` (`DEMO_PORT` to change) |
| `make dev-web` | Vite dev server with hot reload |
| `make test` | Go tests (race detector) + web tests |
| `make lint` | gofmt, vet, tsc, eslint, no-raw-colours, schema validation |
| `make check-schemas` | Validate JSON Schemas and the OpenAPI document |
| `make fmt` | Format Go sources |
| `make clean` | Remove build output |

## 6.3 `yubeosctl` ✅

Local administration tool, run on the server as root. See [Deployment](../DEPLOYMENT.md) and [Authentication §9](../AUTHENTICATION.md#9-bootstrap-recovery-and-administration). Common flags: `--config PATH`, `--socket PATH`, `--state-dir PATH`, `--url https://host`.

| Command | Purpose |
|---|---|
| `yubeosctl user add <linux-user> [--admin] [--ttl MIN]` | Create the account and print a one-time setup link |
| `yubeosctl user reset <linux-user> [--ttl MIN]` | Disable the current password and print a reset link (revokes sessions and trusted devices) |
| `yubeosctl user reset <linux-user> --2fa` | Remove TOTP, recovery codes, passkeys and trusted devices and end sessions; the user re-enrols from the LAN |
| `yubeosctl lockdown on\|off\|status` | Refuse (or allow again) every request from a remote origin immediately |
| `yubeosctl status` | Version, exposure, 2FA policy, lockdown and helper reachability |
| `yubeosctl doctor` | Check the helper, configuration and file permissions |
| `yubeosctl doctor --remote` | Additionally check remote-access readiness (allowed hosts, TLS, native auth, 2FA policy, root login, lockdown) |
| `yubeosctl audit verify` | Verify the audit log hash chain |
| `yubeosctl version` | Print the version |

Planned, not implemented yet 🟦: `export-config` (back up configuration) and `rollback` (restore the previous binary and database backup after a failed upgrade).

## 6.4 Configuration keys ✅

Format: YAML (JSON is accepted too), normally `/etc/yubeos/config.yaml`; see `packaging/config/config.example.yaml`. Machine-readable: [`schemas/config.schema.json`](../../schemas/config.schema.json).

### `server`

| Key | Default | Notes |
|---|---|---|
| `listen` | `:443` | Address of the HTTPS listener |
| `httpRedirect` | `:80` | HTTP→HTTPS redirect listener |
| `exposure` | `lan` | `lan` or `remote` (enables the strict rules in [chapter 4](04-remote-access.md#43-the-safety-net)) |
| `allowedHosts` | `[]` | Accepted `Host` names; mandatory when remote |
| `behindProxy` | `false` | TLS is terminated by a proxy/tunnel in front |
| `trustedProxies` | `[]` | IPs/CIDRs allowed to set forwarded-client headers; required with `behindProxy` |
| `treatOverlayAsLocal` | `true` | Treat `100.64.0.0/10` (Tailscale/CGNAT) as local |
| `tls.mode` | `selfsigned` | `selfsigned`, `files` or `acme` (built-in Let’s Encrypt) |
| `tls.certFile` / `tls.keyFile` | — | For `files` mode |
| `tls.acmeDomains` / `tls.acmeEmail` | — | For `acme` mode |

### `auth`

| Key | Default | Notes |
|---|---|---|
| `mode` | `native` | `native` or `pam` (LAN only) |
| `adminGroup` / `userGroup` | `yubeos-admin` / `yubeos-user` | Linux groups mapped to roles |
| `allowRoot` | `false` | Allow the `root` account on the web UI (forbidden when remote) |
| `sessionIdleMinutes` / `sessionMaxHours` | `30` / `12` | Session lifetime |
| `require2fa` | `admins` | `none`, `admins`, `all` (must be `all` when remote) |
| `trustedDeviceDays` | `30` | `0` disables “remember this device” |
| `allowPasskeyAsSoleFactor` | `true` | A verified passkey counts as both factors |
| `lockoutThreshold` / `lockoutMinutes` | `10` / `15` | Account lockout |
| `remoteIdleMinutes` | `15` | Idle timeout from remote origins |
| `adminRemoteTrustedDeviceDays` | `14` | Cap for admin trusted devices from remote origins |
| `breachCheck` | `false` | Check new passwords against a k-anonymity breach list (outbound call) |

### Other groups

| Group | Keys (defaults) |
|---|---|
| `providers` | `storage`, `gpu` (`auto`, `nvidia`, `none`), `power` (`auto`, `none`; RAPL through the helper) — all `auto` by default |
| `files` | `roots` (`["/mnt"]`), `followSymlinks` (`false`) |
| `terminal` | `allowUsers` (`false`), `requireStepUpWhenRemote` (`true`), `shell`, `maxSessionsPerUser` (`5`), `idleTimeoutMinutes`, `detachedTtlMinutes` (`30`), `recordSessions` |
| `addons` | `enabled` (`true`), `portRange` (`20000-29999`), `catalogs[]` (`id`, `url`, `verify`) |
| `repopapers` | `enabled` (`true`), `githubTokenFile`, `sources` (`github`, `arxiv`, `semanticscholar`, `huggingface`) |
| `logging` | `level` (`info`), `format` (`json`) |
| `access` | `allowCidrs` (`[]` = no restriction; local origins always allowed) |

### Metric series names

Series keys are stable and used by the API and the WebSocket (`prefix.*` patterns select groups):

| Key | Unit |
|---|---|
| `cpu.<socket>.util` · `.temp` · `.freq` · `.power`, `cpu.core.<n>.util` | percent · °C · Hz · W · percent |
| `mem.total` · `used` · `cache` · `arc` · `free` · `swap.used` | bytes |
| `net.<if>.rx` · `.tx` · `.errors` · `.speed` | bytes/s · bytes/s · count · bit/s |
| `gpu.<n>.util` · `.temp` · `.vram.used` · `.vram.total` · `.power` · `.power.limit` · `.fan` | percent · °C · bytes · bytes · W · W · percent |
| `disk.<name>.read` · `.write` · `.iops` · `.util` · `.temp` | bytes/s · bytes/s · count/s · percent · °C |
| `sys.load1` · `load5` · `load15` · `uptime` | load · seconds |

### Minimal real-server example

```yaml
server:
  listen: ":443"
  allowedHosts: ["nas.local"]
auth:
  require2fa: admins
  trustedDeviceDays: 30
files:
  roots: ["/mnt", "/tank"]
```

## 6.5 Files and paths 🟦

| Path | Content |
|---|---|
| `/etc/yubeos/config.yaml` | Configuration |
| `/var/lib/yubeos/` | Database, keys, backups (owned by `yubeos`) |
| `/var/lib/yubeos-priv/` | Credential store and secrets (root only; unreadable by `yubeosd`) |
| `/run/yubeos/priv.sock` | Local socket between `yubeosd` and `yubeos-priv` |

Next: [7. Troubleshooting and FAQ](07-troubleshooting.md).
