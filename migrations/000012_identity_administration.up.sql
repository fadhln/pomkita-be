-- BA1 identity administration tables and constraints.

ALTER TABLE users
    ADD COLUMN username text,
    ADD COLUMN invited_at timestamptz(6),
    ADD COLUMN activated_at timestamptz(6),
    ADD COLUMN updated_at timestamptz(6);

ALTER TABLE users
    ALTER COLUMN password_hash DROP NOT NULL;

ALTER TABLE users
    ADD CONSTRAINT users_enabled_identity_check
    CHECK (NOT enabled OR (username IS NOT NULL AND btrim(username) <> '' AND password_hash IS NOT NULL AND btrim(password_hash) <> ''));

CREATE UNIQUE INDEX users_username_ci ON users (lower(username));

CREATE TABLE account_tokens (
    token_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    user_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('invitation', 'password_reset')),
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz(6) NOT NULL,
    consumed_at timestamptz(6),
    created_by uuid,
    created_at timestamptz(6) NOT NULL,
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, user_id),
    FOREIGN KEY (created_by) REFERENCES users (user_id)
);

CREATE UNIQUE INDEX account_tokens_one_open_per_user_purpose
    ON account_tokens (user_id, purpose)
    WHERE consumed_at IS NULL;

CREATE INDEX account_tokens_token_hash ON account_tokens (token_hash);
