-- 0002_auth.sql: Password auth + multi-device sessions.
--
-- password_hash defaults to '' for any pre-existing row; bcrypt will never
-- successfully validate against an empty hash, so migrated-in-place users
-- are locked out until an admin sets a real password (or re-runs bootstrap).
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';

-- One row per authenticated device/browser. A user may hold many concurrent
-- sessions by design — there is no unique constraint on user_id, so logging
-- in from a desktop, a phone, and a tablet at once is the normal case, not
-- an edge case.
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL, -- SHA-256 of the bearer token; the raw token only ever lives in the cookie
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_label TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
