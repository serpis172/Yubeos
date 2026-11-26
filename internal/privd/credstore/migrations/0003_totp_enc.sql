-- TOTP secrets are encrypted at rest (AES-256-GCM, key derived from pepper.key). enc=0 rows are legacy plaintext
-- and are re-encrypted the first time they are read.
ALTER TABLE totp ADD COLUMN enc INTEGER NOT NULL DEFAULT 0;
