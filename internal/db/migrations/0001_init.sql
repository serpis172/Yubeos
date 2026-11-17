-- yubeos.db — core tables (docs/DATA-MODEL.md). Credential material is NOT stored here: it lives in the
-- helper-owned store (internal/privd/credstore).

CREATE TABLE settings(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL CHECK(json_valid(value)),
  updated_at INTEGER NOT NULL);

CREATE TABLE users_meta(
  username TEXT PRIMARY KEY,
  uid INTEGER NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('admin','user')),
  enabled INTEGER NOT NULL DEFAULT 1,
  has_2fa INTEGER NOT NULL DEFAULT 0,
  last_login_at INTEGER,
  created_at INTEGER NOT NULL);

CREATE TABLE trusted_devices(
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE,
  prev_token_hash BLOB,
  prev_valid_until INTEGER,
  label TEXT NOT NULL,
  ua_family TEXT, ua_major INTEGER, os_family TEXT,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL,
  last_ip TEXT,
  last_2fa_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  revoked_at INTEGER,
  revoke_reason TEXT);
CREATE INDEX idx_devices_user ON trusted_devices(username);

CREATE TABLE sessions(
  id_hash BLOB PRIMARY KEY,                 -- SHA-256 of the cookie value
  sid TEXT NOT NULL UNIQUE,                 -- public identifier used by the UI/API for listing and revocation
  username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  csrf TEXT NOT NULL,
  ip TEXT, user_agent TEXT,
  origin TEXT NOT NULL CHECK(origin IN ('local','remote')),
  factors TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(factors)),
  enrollment_required INTEGER NOT NULL DEFAULT 0,
  cred_epoch INTEGER NOT NULL DEFAULT 0,    -- credential epoch at login; a bump in the helper store kills the session
  device_id TEXT REFERENCES trusted_devices(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  rotated_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  prev_id_hash BLOB,                        -- accepted briefly after rotation (parallel requests)
  prev_valid_until INTEGER,
  reauth_until INTEGER);
CREATE INDEX idx_sessions_user ON sessions(username);
CREATE INDEX idx_sessions_prev ON sessions(prev_id_hash);

CREATE TABLE login_failures(
  ts INTEGER NOT NULL,
  username TEXT,
  ip TEXT NOT NULL,
  reason TEXT NOT NULL);                    -- 'bad-credentials' | 'ok' (success marker) | ...
CREATE INDEX idx_failures_ip ON login_failures(ip, ts);
CREATE INDEX idx_failures_user ON login_failures(username, ts);

CREATE TABLE ip_bans(
  ip TEXT PRIMARY KEY,
  until INTEGER NOT NULL,
  strikes INTEGER NOT NULL DEFAULT 1,
  reason TEXT);

CREATE TABLE api_tokens(
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash BLOB NOT NULL UNIQUE,
  scopes TEXT NOT NULL CHECK(json_valid(scopes)),
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  last_used_at INTEGER);

CREATE TABLE audit_log(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  actor TEXT, role TEXT, ip TEXT, req_id TEXT,
  action TEXT NOT NULL,
  target TEXT,
  params TEXT CHECK(params IS NULL OR json_valid(params)),
  result TEXT NOT NULL CHECK(result IN ('ok','fail','denied')),
  job_id TEXT,
  prev_hash BLOB,
  hash BLOB NOT NULL);
CREATE INDEX idx_audit_ts ON audit_log(ts);

CREATE TABLE jobs(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  actor TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
  params TEXT CHECK(params IS NULL OR json_valid(params)),
  progress REAL NOT NULL DEFAULT 0,
  message TEXT, log_tail TEXT, error TEXT,
  created_at INTEGER NOT NULL, started_at INTEGER, finished_at INTEGER);
CREATE INDEX idx_jobs_state ON jobs(state, created_at);
