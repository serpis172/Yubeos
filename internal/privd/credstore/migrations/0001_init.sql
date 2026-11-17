-- credentials.db — owned by the root helper (never readable by yubeosd). docs/AUTHENTICATION.md §3.
CREATE TABLE settings(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL CHECK(json_valid(value)));

CREATE TABLE credentials(
  username TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL,              -- PHC string, Argon2id over HMAC-SHA256(pepper, NFKC(password))
  epoch INTEGER NOT NULL DEFAULT 1,         -- bumped on every credential change; sessions older than it die
  pending_reset INTEGER NOT NULL DEFAULT 0, -- 1 = password disabled until a setup link is consumed
  updated_at INTEGER NOT NULL);

CREATE TABLE setup_links(
  token_hash BLOB PRIMARY KEY,              -- SHA-256 of the 256-bit random token
  username TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK(purpose IN ('first-password','reset')),
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER);
CREATE INDEX idx_setup_user ON setup_links(username);
