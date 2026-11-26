-- TOTP and recovery codes (docs/AUTHENTICATION.md §3, §4.1). Only the helper reads this file.
CREATE TABLE totp(
  username TEXT PRIMARY KEY,
  secret_enc BLOB NOT NULL,                 -- nonce(12) || AES-256-GCM(secret.key, AAD = username)
  active INTEGER NOT NULL DEFAULT 0,        -- 0 = generated, waiting for the confirming code
  last_step INTEGER NOT NULL DEFAULT 0,     -- last accepted 30 s time step: each step is accepted once (replay)
  created_at INTEGER NOT NULL);

CREATE TABLE recovery_codes(
  username TEXT NOT NULL,
  hash BLOB NOT NULL,                       -- SHA-256 of the normalised code; the code itself is shown once
  used_at INTEGER,
  PRIMARY KEY(username, hash)) WITHOUT ROWID;
