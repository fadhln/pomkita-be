-- Phase 2 authentication tables. Business authentication stays in Go.

ALTER TABLE users
    ADD COLUMN email text NOT NULL CHECK (btrim(email) <> ''),
    ADD COLUMN password_hash text NOT NULL CHECK (btrim(password_hash) <> '');

CREATE UNIQUE INDEX users_email_ci ON users (lower(email));

CREATE TABLE jwt_keys (
    kid text PRIMARY KEY CHECK (btrim(kid) <> ''),
    secret_ref text NOT NULL CHECK (btrim(secret_ref) <> ''),
    status text NOT NULL CHECK (status IN ('active', 'previous', 'retired')),
    activated_at timestamptz(6) NOT NULL,
    max_token_expiry timestamptz(6) NOT NULL,
    CHECK (max_token_expiry >= activated_at)
);

CREATE UNIQUE INDEX jwt_keys_one_active ON jwt_keys ((status)) WHERE status = 'active';

CREATE TABLE sessions (
    jti uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (user_id),
    kid text NOT NULL REFERENCES jwt_keys (kid),
    issued_at timestamptz(6) NOT NULL,
    expires_at timestamptz(6) NOT NULL,
    last_active_at timestamptz(6) NOT NULL,
    revoked_at timestamptz(6),
    CHECK (expires_at >= issued_at),
    CHECK (expires_at - issued_at <= interval '15 minutes'),
    CHECK (last_active_at >= issued_at)
);

CREATE INDEX sessions_user_id ON sessions (user_id);
