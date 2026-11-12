# Module: Terminal

Route `/terminal` · Roles: admin, user (if `terminal.allowUsers`) · Backend: `internal/terminal`, `internal/privd` (`pty.spawn`) · API: [`/terminal/*`, `/ws/terminal/{id}`](../API.md#terminal)

## 1. Features (v1)

- Multiple tabs, each an independent PTY; rename, close, duplicate.
- Real login-like shell for the authenticated Linux user (`$SHELL` from `/etc/passwd`, overridable), `$TERM=xterm-256color`, UTF-8, correct `HOME`, supplementary groups, `sudo` works.
- xterm.js with fit, search, web-links and WebGL renderer (canvas fallback); copy/paste, configurable font (follows Appearance), scrollback 10 000 lines, bell off by default.
- **Survives page reload / network blips:** the server keeps the PTY alive and a 1 MiB output ring buffer per session; the UI re-attaches and replays.
- Resize, Ctrl-C/Ctrl-D semantics, bracketed paste, OSC 52 clipboard *disabled* by default.
- Idle timeout (optional), max sessions per user, admin can list/kill sessions.

Non-goals v1: session recording UI, file transfer in terminal (use File Explorer), split panes.

## 2. Wire protocol (`/ws/terminal/{id}`)

| Frame | Direction | Content |
|---|---|---|
| Binary | both | raw PTY bytes (client→server: keystrokes; server→client: output) |
| Text JSON | client→server | `{"t":"resize","cols":120,"rows":32}` · `{"t":"ping"}` |
| Text JSON | server→client | `{"t":"exit","code":0}` · `{"t":"replay","bytes":N}` · `{"t":"pong"}` |

Auth: same session cookie, `Origin` check, session must own the terminal (or be admin with explicit audit flag `adminAttach`). Heartbeat every 20 s; server closes after 60 s silence (PTY kept alive for re-attach for `terminal.detachedTtlMinutes`, default 30).

## 3. Process model ([ADR-0014](../adr/0014-terminal-spawn.md))

Constraint discovered during design: if the PTY shell were a child of the hardened helper, it would inherit its sandbox (notably any `NoNewPrivileges`), breaking `sudo`/`su`. Therefore:

```
yubeosd ──pty.spawn(user, cols, rows)──▶ yubeos-priv
yubeos-priv: allocates PTY pair, starts
   systemd-run --quiet --collect --uid=<user> --property=PAMName=login \
               --property=StandardInput=fd... (slave) --setenv=TERM=xterm-256color  -- <shell> -l
returns master FD to yubeosd over the Unix socket (SCM_RIGHTS)
yubeosd bridges master FD ⇄ WebSocket, owns ring buffer and lifecycle
```

Benefits: session appears in `loginctl`, PAM session modules run (limits, motd optional), shell runs in a clean cgroup, no inherited sandbox, `sudo` works, kill = stop the transient unit.

**Spike S-3 (Phase 3, blocking):** validate this approach against the fallback `fork + Credential + setsid + TIOCSCTTY` on Ubuntu 24.04 and Debian 13. Pass criteria: `sudo -v` works; `tty` prints a pts; resize propagates (`stty size`); `loginctl list-sessions` shows the session; killing the browser tab leaves the process running; no fd leaks to the shell. The result is recorded in ADR-0014 before the module is implemented.

## 4. Security notes

See [SECURITY §6](../SECURITY.md#6-module-specific-controls). Terminal input/output is never logged by default; open/close/attach events are audited with user, IP and unit name.

## 5. Acceptance criteria (Phase 3)

1. `vim`, `htop`, `tmux`, `top`, `less`, 256-colour and UTF-8 test pages render correctly.
2. Reload the page mid-`ping`: session re-attaches, output continues, no duplicated bytes beyond the replay marker.
3. 10 concurrent sessions, one flooding output (`yes`): UI of others stays responsive (back-pressure drops *only* the flooding client's backlog).
4. Disconnect network 10 s: reconnect succeeds; 40 min: session expired per TTL and cleaned up (unit stopped).
5. Non-admin without permission gets `403`; no unit is created.
