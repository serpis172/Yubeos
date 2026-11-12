# Data Model

Engine: **SQLite** (WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`) via a pure-Go driver (no CGO in `yubeosd`) — [ADR-0005](adr/0005-sqlite.md). File: `/var/lib/yubeos/yubeos.db`, mode `0600`, owner `yubeos`.

## 1. Rules

- Migrations are embedded, forward-only, numbered `NNNN_name.sql`, applied in a transaction at start-up; the applied version is stored in `schema_migrations`. A DB newer than the binary ⇒ refuse to start.
- IDs: ULID text for generated rows; natural keys where stable (disk id, theme id).
- Timestamps: INTEGER Unix milliseconds UTC (`*_at`).
- JSON columns hold validated JSON (`CHECK(json_valid(col))`).
- Secrets: the daemon DB holds only hashes of random tokens (sessions, trusted devices, API tokens, setup links). **Password verifiers, TOTP secrets, passkey public keys and recovery-code hashes are NOT in this database**: they live in the helper-owned store `/var/lib/yubeos-priv/credentials.db` (root `0600`, schema in `internal/privd/credstore`, same migration rules, AES-256-GCM for TOTP secrets with `/var/lib/yubeos-priv/secret.key`).
- No cross-module foreign keys except to `users_meta`; modules own their tables (prefix per module).
- Backups: nightly `VACUUM INTO /var/lib/yubeos/backups/yubeos-YYYYMMDD.db` (7 kept) + before every migration.

## 2. Tables

```sql
-- core
CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL CHECK(json_valid(value)), updated_at INTEGER NOT NULL);

CREATE TABLE users_meta(            -- Yubeos-side data about a web account (credentials are in the helper store)
  username TEXT PRIMARY KEY, uid INTEGER NOT NULL, role TEXT NOT NULL CHECK(role IN ('admin','user')),
  enabled INTEGER NOT NULL DEFAULT 1, has_2fa INTEGER NOT NULL DEFAULT 0,   -- cached flags mirrored from the helper
  last_login_at INTEGER, created_at INTEGER NOT NULL);

CREATE TABLE sessions(
  id_hash BLOB PRIMARY KEY,         -- SHA-256 of session id
  username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  csrf TEXT NOT NULL, ip TEXT, user_agent TEXT,
  created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, reauth_until INTEGER,
  origin TEXT NOT NULL CHECK(origin IN ('local','remote')), device_id TEXT REFERENCES trusted_devices(id) ON DELETE SET NULL,
  factors TEXT NOT NULL DEFAULT '[]');
CREATE INDEX idx_sessions_user ON sessions(username);

CREATE TABLE trusted_devices(
  id TEXT PRIMARY KEY, username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE, prev_token_hash BLOB,            -- prev kept 10 min to detect reuse of a rotated token
  label TEXT NOT NULL, ua_family TEXT, ua_major INTEGER, os_family TEXT,
  created_at INTEGER NOT NULL, last_used_at INTEGER NOT NULL, last_ip TEXT, last_2fa_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL, revoked_at INTEGER, revoke_reason TEXT);
CREATE INDEX idx_devices_user ON trusted_devices(username);

CREATE TABLE login_failures(ts INTEGER NOT NULL, username TEXT, ip TEXT NOT NULL, reason TEXT NOT NULL);
CREATE INDEX idx_failures_ip ON login_failures(ip, ts);
CREATE INDEX idx_failures_user ON login_failures(username, ts);
CREATE TABLE ip_bans(ip TEXT PRIMARY KEY, until INTEGER NOT NULL, strikes INTEGER NOT NULL DEFAULT 1, reason TEXT);
CREATE TABLE auth_challenges(id_hash BLOB PRIMARY KEY, username TEXT NOT NULL, ip_prefix TEXT, ua_family TEXT, attempts INTEGER NOT NULL DEFAULT 0, expires_at INTEGER NOT NULL);
CREATE TABLE setup_links(token_hash BLOB PRIMARY KEY, username TEXT NOT NULL, purpose TEXT NOT NULL, expires_at INTEGER NOT NULL, used_at INTEGER);

CREATE TABLE api_tokens(
  id TEXT PRIMARY KEY, username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  name TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE, scopes TEXT NOT NULL,
  created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, last_used_at INTEGER);

CREATE TABLE audit_log(
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, actor TEXT, role TEXT, ip TEXT, req_id TEXT,
  action TEXT NOT NULL, target TEXT, params TEXT CHECK(params IS NULL OR json_valid(params)),
  result TEXT NOT NULL CHECK(result IN ('ok','fail','denied')), job_id TEXT, prev_hash BLOB, hash BLOB NOT NULL);
CREATE INDEX idx_audit_ts ON audit_log(ts);

CREATE TABLE jobs(
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, actor TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
  params TEXT, progress REAL NOT NULL DEFAULT 0, message TEXT, log_tail TEXT, error TEXT,
  created_at INTEGER NOT NULL, started_at INTEGER, finished_at INTEGER);
CREATE INDEX idx_jobs_state ON jobs(state, created_at);

-- dashboard
CREATE TABLE metrics_rollup(series TEXT NOT NULL, ts_min INTEGER NOT NULL, min REAL, avg REAL, max REAL, PRIMARY KEY(series, ts_min)) WITHOUT ROWID;

-- storage / shares
CREATE TABLE disk_meta(disk_id TEXT PRIMARY KEY, alias TEXT, tags TEXT NOT NULL DEFAULT '[]', first_seen_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
CREATE TABLE shares(
  id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('smb','nfs')), name TEXT NOT NULL, path TEXT NOT NULL,
  options TEXT NOT NULL CHECK(json_valid(options)), enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, UNIQUE(kind, name));
CREATE TABLE snapshot_policies(id TEXT PRIMARY KEY, dataset TEXT NOT NULL, schedule TEXT NOT NULL, keep TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1);

-- add-ons
CREATE TABLE addon_catalogs(id TEXT PRIMARY KEY, url TEXT NOT NULL, verify INTEGER NOT NULL, last_refresh_at INTEGER, last_error TEXT);
CREATE TABLE addon_catalog_entries(catalog_id TEXT NOT NULL REFERENCES addon_catalogs(id) ON DELETE CASCADE, addon_id TEXT NOT NULL, version TEXT NOT NULL, manifest TEXT NOT NULL CHECK(json_valid(manifest)), PRIMARY KEY(catalog_id, addon_id, version));
CREATE TABLE addons_installed(
  id TEXT PRIMARY KEY, catalog_id TEXT, version TEXT NOT NULL, desired_state TEXT NOT NULL CHECK(desired_state IN ('running','stopped')),
  config TEXT NOT NULL, approvals TEXT NOT NULL DEFAULT '[]', host_ports TEXT NOT NULL DEFAULT '{}',
  previous_digests TEXT, installed_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);

-- repo-papers
CREATE TABLE rp_topics(id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, spec TEXT NOT NULL CHECK(json_valid(spec)), builtin INTEGER NOT NULL DEFAULT 0);
CREATE TABLE rp_repos(id INTEGER PRIMARY KEY /* GitHub node db id */, full_name TEXT NOT NULL, description TEXT, language TEXT, license TEXT, topics TEXT, created_at INTEGER, pushed_at INTEGER, archived INTEGER, fork INTEGER, stars INTEGER, forks INTEGER, fetched_at INTEGER NOT NULL);
CREATE TABLE rp_repo_snapshots(repo_id INTEGER NOT NULL REFERENCES rp_repos(id) ON DELETE CASCADE, day TEXT NOT NULL /* YYYY-MM-DD */, stars INTEGER NOT NULL, forks INTEGER NOT NULL, open_issues INTEGER, pushed_at INTEGER, PRIMARY KEY(repo_id, day)) WITHOUT ROWID;
CREATE TABLE rp_papers(id TEXT PRIMARY KEY /* arxiv:2401.01234 or doi:… */, title TEXT NOT NULL, authors TEXT, abstract TEXT, categories TEXT, venue TEXT, tier TEXT CHECK(tier IN ('T1','T2','T3')), published_at INTEGER NOT NULL, hf_upvotes INTEGER, citations INTEGER, influential_citations INTEGER, linked_repo TEXT, urls TEXT, fetched_at INTEGER NOT NULL);
CREATE TABLE rp_user_state(username TEXT NOT NULL, item_kind TEXT NOT NULL CHECK(item_kind IN ('repo','paper')), item_id TEXT NOT NULL, saved INTEGER NOT NULL DEFAULT 0, hidden INTEGER NOT NULL DEFAULT 0, read INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(username, item_kind, item_id));
CREATE TABLE rp_source_state(source TEXT PRIMARY KEY, etag TEXT, last_ok_at INTEGER, last_error TEXT, rate_remaining INTEGER, rate_reset_at INTEGER);

-- appearance
CREATE TABLE themes(id TEXT PRIMARY KEY, owner TEXT, name TEXT NOT NULL, data TEXT NOT NULL CHECK(json_valid(data)), created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE ui_preferences(username TEXT PRIMARY KEY, data TEXT NOT NULL CHECK(json_valid(data)), version INTEGER NOT NULL DEFAULT 1, updated_at INTEGER NOT NULL);
CREATE TABLE fonts(id TEXT PRIMARY KEY, family TEXT NOT NULL, file TEXT NOT NULL, sha256 BLOB NOT NULL, size INTEGER NOT NULL, uploaded_by TEXT, uploaded_at INTEGER NOT NULL);
```

## 3. Retention

| Data | Retention |
|---|---|
| `sessions`, `auth_challenges`, `setup_links` | until expiry (+ hourly purge) |
| `trusted_devices` | until `expires_at`; revoked rows kept 30 d for audit |
| `login_failures` | 30 d |
| `ip_bans` | until `until` (+ daily purge) |
| `audit_log` | 365 d default (configurable); purge keeps hash chain valid by writing a checkpoint row |
| `jobs` | 90 d (failed 180 d) |
| `metrics_rollup` | 30 d |
| `rp_repo_snapshots` | 400 d |
| `rp_repos` / `rp_papers` not referenced and older than 1 y | purged weekly |

## 4. Migration policy

Never edit an applied migration. Destructive migrations (drop/rename) need a two-step release (add+backfill, then remove one minor later). Each migration has a Go test that upgrades a fixture DB from every previous schema version.
