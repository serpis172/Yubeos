# Architecture Decision Records

We record decisions that are expensive to reverse. Status values: **Proposed** (needs a spike or review) → **Accepted** → **Superseded by ADR-N**.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-monorepo.md) | Single monorepo | Accepted |
| [0002](0002-go-backend.md) | Go for backend and helper | Accepted |
| [0003](0003-privilege-separation.md) | Unprivileged daemon + root helper with typed RPC | Accepted |
| [0004](0004-react-spa.md) | React + TypeScript SPA, no component kit | Accepted |
| [0005](0005-sqlite.md) | SQLite as the only database | Accepted |
| [0006](0006-zfs-primary.md) | OpenZFS primary storage engine behind a Backend interface | Accepted |
| [0007](0007-rest-ws-openapi.md) | REST + WebSocket, OpenAPI source of truth | Accepted |
| [0008](0008-compose-addons.md) | Docker Compose add-on runtime | Accepted |
| [0009](0009-distribution.md) | Ship as a layer (.deb/apt) first | Accepted |
| [0010](0010-auth.md) | Linux accounts via PAM, sessions, TOTP | Superseded by 0016 |
| [0011](0011-theming.md) | Data-only themes → CSS tokens | Accepted |
| [0012](0012-demo-mode.md) | Provider interfaces + demo mode | Accepted |
| [0013](0013-dependency-policy.md) | Dependency policy | Accepted |
| [0014](0014-terminal-spawn.md) | Terminal as systemd transient units | **Proposed** (spike S-3) |
| [0015](0015-fsworker.md) | Per-session file workers | **Proposed** (spike S-2) |
| [0016](0016-native-credentials.md) | Native credentials, passkeys, TOTP, trusted devices | Accepted |
| [0017](0017-remote-access.md) | Remote access via exposure modes | Accepted |

## Template

```markdown
# ADR-NNNN: Title
**Status:** Proposed | Accepted | Superseded by ADR-N · **Date:** YYYY-MM-DD
## Context      (forces, constraints)
## Decision     (what we do, precisely)
## Consequences (good, bad, follow-ups)
## Alternatives considered
```
