-- Reverse BA1 identity administration changes only.

DROP TABLE IF EXISTS account_tokens;
DROP INDEX IF EXISTS users_username_ci;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_enabled_identity_check;

ALTER TABLE users
    ALTER COLUMN password_hash SET NOT NULL;

ALTER TABLE users
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS activated_at,
    DROP COLUMN IF EXISTS invited_at,
    DROP COLUMN IF EXISTS username;
