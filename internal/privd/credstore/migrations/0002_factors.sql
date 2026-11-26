-- Second factors held by the root helper: TOTP secrets and recovery codes (docs/AUTHENTICATION.md §4).
CREATE TABLE totp(
  username TEXT PRIMARY KEY,
  secret BLOB NOT NULL,
  confirmed INTEGER NOT NULL DEFAULT 0,     -- 0 while pending enrolment (first code not yet confirmed)
  last_step INTEGER NOT NULL DEFAULT 0,     -- highest accepted time step: each step is usable once
  created_at INTEGER NOT NULL);

CREATE TABLE recovery_codes(
  username TEXT NOT NULL,
  code_hash BLOB NOT NULL,                  -- HMAC-SHA256(pepper, normalised code)
  used_at INTEGER,
  PRIMARY KEY(username, code_hash));
