-- WebAuthn credentials: public-key material only (no secret), so they live in the daemon database.
CREATE TABLE passkeys(
  id BLOB PRIMARY KEY,                      -- credential ID
  username TEXT NOT NULL REFERENCES users_meta(username) ON DELETE CASCADE,
  public_key BLOB NOT NULL,                 -- uncompressed P-256 point
  sign_count INTEGER NOT NULL DEFAULT 0,
  label TEXT NOT NULL,
  rp_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  flagged INTEGER NOT NULL DEFAULT 0);      -- 1 = signature-counter regression seen: login refused
CREATE INDEX idx_passkeys_user ON passkeys(username);
