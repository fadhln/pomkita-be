-- Phase 5 audit tables. Audit chain writes stay in Go service transactions.

CREATE TABLE audit_chain_locks (
    org_id uuid PRIMARY KEY REFERENCES organizations (org_id)
);

CREATE TABLE audit_log (
    event_id uuid PRIMARY KEY,
    org_id uuid NOT NULL,
    org_sequence bigint NOT NULL CHECK (org_sequence > 0),
    event_type text NOT NULL CHECK (btrim(event_type) <> ''),
    payload jsonb NOT NULL,
    outcome text NOT NULL CHECK (btrim(outcome) <> ''),
    outcome_error text,
    created_at timestamptz(6) NOT NULL,
    prev_hash bytea NOT NULL CHECK (octet_length(prev_hash) = 32),
    row_hash bytea NOT NULL CHECK (octet_length(row_hash) = 32),
    UNIQUE (org_id, event_id),
    UNIQUE (org_id, org_sequence),
    FOREIGN KEY (org_id) REFERENCES organizations (org_id)
);

CREATE TABLE audit_outbox (
    org_id uuid NOT NULL,
    event_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz(6) NOT NULL,
    PRIMARY KEY (org_id, event_id),
    FOREIGN KEY (org_id, event_id) REFERENCES audit_log (org_id, event_id)
);

CREATE TABLE audit_denied (
    request_id uuid PRIMARY KEY,
    sub uuid,
    jti uuid,
    org_id uuid,
    station_id uuid,
    action text NOT NULL CHECK (btrim(action) <> ''),
    target text NOT NULL CHECK (btrim(target) <> ''),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    server_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    outcome text NOT NULL CHECK (btrim(outcome) <> ''),
    error_detail text
);
