DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS jwt_keys_one_active;
DROP TABLE IF EXISTS jwt_keys;
DROP INDEX IF EXISTS users_email_ci;
ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS email;
