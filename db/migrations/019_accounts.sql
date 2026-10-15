-- Account credentials for the opt-in private-library mode (migration 019).
--
-- Additive only and idempotent. Legacy rows seeded by 008_identity.sql
-- ('local' and 'system') keep password_hash NULL and therefore cannot log
-- in with a password unless the offline bootstrap explicitly adopts one of
-- them as the initial owner. Existing rows, handles, and saved-search
-- owner_actor keys are untouched.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS password_hash TEXT
        CHECK (password_hash IS NULL OR length(btrim(password_hash)) BETWEEN 1 AND 256);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS disabled BOOLEAN NOT NULL DEFAULT false;

-- Login and admin-list lookups filter on disabled accounts.
CREATE INDEX IF NOT EXISTS users_active_handle_idx
    ON users (handle) WHERE NOT disabled;

-- Session resolution joins on user and expiry; the 008 index covers it, but
-- token-hash lookup is the hot path for every private request.
CREATE INDEX IF NOT EXISTS sessions_token_hash_idx
    ON sessions (token_hash) WHERE revoked_at IS NULL;
