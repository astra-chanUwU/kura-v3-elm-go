-- Public profiles and list creators.
--
-- Idempotent and additive. The login handle and actor key stay on users.handle.
-- username is the mutable public name used by /u/USERNAME. It is not the
-- login handle and it is not an actor id. local and system stay reserved so
-- adopted legacy handles keep ActorIDForUser stable.
--
-- collections.created_by is NULL for every list that already exists. Nothing
-- here assigns those rows to an account.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS username TEXT
        CHECK (
            username IS NULL
            OR (
                username ~ '^[a-z0-9][a-z0-9_-]{0,31}$'
                AND username <> ALL (ARRAY[
                    'local', 'system', 'sign-in', 'sign-up', 'trouble',
                    'api', 'media', 'health', 'static', 'dist', 'u',
                    'profile', 'profiles', 'index', 'assets', 'icons',
                    'brand', 'favicon'
                ]::text[])
            )
        );

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS display_name TEXT
        CHECK (display_name IS NULL OR char_length(btrim(display_name)) BETWEEN 1 AND 50);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio TEXT
        CHECK (bio IS NULL OR char_length(bio) BETWEEN 1 AND 160);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS website TEXT
        CHECK (website IS NULL OR char_length(website) BETWEEN 1 AND 200);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS avatar_key TEXT
        CHECK (avatar_key IS NULL OR char_length(avatar_key) BETWEEN 1 AND 240);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS profile_version BIGINT NOT NULL DEFAULT 1
        CHECK (profile_version >= 1);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_key
    ON users (username) WHERE username IS NOT NULL;

UPDATE users AS u
SET username = lower(u.handle)
WHERE u.username IS NULL
  AND lower(u.handle) ~ '^[a-z0-9][a-z0-9_-]{0,31}$'
  AND lower(u.handle) <> ALL (ARRAY[
      'local', 'system', 'sign-in', 'sign-up', 'trouble',
      'api', 'media', 'health', 'static', 'dist', 'u',
      'profile', 'profiles', 'index', 'assets', 'icons',
      'brand', 'favicon'
  ]::text[])
  AND NOT EXISTS (
      SELECT 1
      FROM users AS other
      WHERE other.id <> u.id
        AND lower(other.handle) = lower(u.handle)
  );

CREATE OR REPLACE FUNCTION users_assign_profile_username()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    candidate text := lower(btrim(NEW.handle));
    reserved text[] := ARRAY[
        'local', 'system', 'sign-in', 'sign-up', 'trouble',
        'api', 'media', 'health', 'static', 'dist', 'u',
        'profile', 'profiles', 'index', 'assets', 'icons',
        'brand', 'favicon'
    ];
BEGIN
    IF NEW.profile_version IS NULL OR NEW.profile_version < 1 THEN
        NEW.profile_version := 1;
    END IF;
    IF NEW.username IS NULL
       AND candidate ~ '^[a-z0-9][a-z0-9_-]{0,31}$'
       AND NOT (candidate = ANY (reserved))
       AND NOT EXISTS (SELECT 1 FROM users WHERE username = candidate) THEN
        NEW.username := candidate;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_assign_profile_username ON users;
CREATE TRIGGER users_assign_profile_username
    BEFORE INSERT ON users
    FOR EACH ROW
    EXECUTE FUNCTION users_assign_profile_username();

ALTER TABLE collections
    ADD COLUMN IF NOT EXISTS created_by BIGINT NULL REFERENCES users (id);

CREATE INDEX IF NOT EXISTS collections_created_by_idx
    ON collections (created_by, created_at DESC, id DESC)
    WHERE created_by IS NOT NULL;
