-- Per-user interface preferences (theme, language, units, custom themes), synced by the web UI.
CREATE TABLE user_prefs(
  username TEXT PRIMARY KEY REFERENCES users_meta(username) ON DELETE CASCADE,
  data TEXT NOT NULL,                       -- validated JSON, at most 128 KiB
  updated_at INTEGER NOT NULL);
