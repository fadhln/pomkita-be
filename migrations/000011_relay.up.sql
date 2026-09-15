-- Phase 5 relay state. Claim and delivery decisions stay in the relay service.

CREATE TABLE outbox_relay_state (
    org_id uuid NOT NULL,
    event_id uuid NOT NULL,
    relay_status text NOT NULL DEFAULT 'pending' CHECK (relay_status IN ('pending', 'in_flight', 'failed', 'delivered')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_token uuid,
    lease_expires_at timestamptz(6),
    last_attempt_at timestamptz(6),
    next_attempt_at timestamptz(6),
    delivered_at timestamptz(6),
    last_error text,
    PRIMARY KEY (org_id, event_id),
    FOREIGN KEY (org_id, event_id) REFERENCES audit_outbox (org_id, event_id),
    CHECK ((relay_status = 'in_flight' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL) OR relay_status <> 'in_flight'),
    CHECK ((relay_status = 'delivered' AND delivered_at IS NOT NULL) OR relay_status <> 'delivered')
);
