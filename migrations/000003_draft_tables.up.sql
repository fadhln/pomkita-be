-- Phase 1 draft child tables and submit idempotency state.

CREATE TABLE draft_readings (
    row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    draft_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    meter_start numeric(10,1) NOT NULL CHECK (meter_start >= 0),
    meter_end numeric(10,1) NOT NULL CHECK (meter_end >= 0),
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, draft_id, nozzle_id),
    UNIQUE (org_id, station_id, draft_id, row_id),
    FOREIGN KEY (org_id, station_id, draft_id)
        REFERENCES shift_drafts (org_id, station_id, draft_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE draft_sales (
    row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    draft_id uuid NOT NULL,
    dispenser_id uuid NOT NULL,
    cash_amount numeric(14,0) NOT NULL CHECK (cash_amount >= 0),
    cashless_amount numeric(14,0) NOT NULL DEFAULT 0 CHECK (cashless_amount >= 0),
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, draft_id, dispenser_id),
    UNIQUE (org_id, station_id, draft_id, row_id),
    FOREIGN KEY (org_id, station_id, draft_id)
        REFERENCES shift_drafts (org_id, station_id, draft_id),
    FOREIGN KEY (org_id, station_id, dispenser_id)
        REFERENCES dispensers (org_id, station_id, dispenser_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE draft_losses (
    row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    draft_id uuid NOT NULL,
    loss_id uuid NOT NULL,
    direction text NOT NULL CHECK (direction IN ('loss', 'gain')),
    reason_code text NOT NULL CHECK (btrim(reason_code) <> ''),
    liters numeric(8,2) NOT NULL CHECK (liters >= 0),
    cash_amount numeric(14,0) CHECK (cash_amount >= 0),
    note text,
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, draft_id, loss_id),
    UNIQUE (org_id, station_id, draft_id, row_id),
    FOREIGN KEY (org_id, station_id, draft_id)
        REFERENCES shift_drafts (org_id, station_id, draft_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE draft_evidence_staging (
    row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    draft_id uuid NOT NULL,
    loss_row_id uuid NOT NULL,
    evidence_type text NOT NULL CHECK (btrim(evidence_type) <> ''),
    object_key text NOT NULL CHECK (btrim(object_key) <> ''),
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    mime text NOT NULL CHECK (btrim(mime) <> ''),
    status text NOT NULL CHECK (status IN ('uploaded', 'verified', 'finalized')),
    uploaded_by uuid NOT NULL,
    UNIQUE (org_id, station_id, draft_id, row_id),
    FOREIGN KEY (org_id, station_id, draft_id, loss_row_id)
        REFERENCES draft_losses (org_id, station_id, draft_id, row_id),
    FOREIGN KEY (org_id, uploaded_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE submit_idempotency (
    idem_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    status text NOT NULL CHECK (status IN ('in_progress', 'succeeded', 'failed')),
    claim_token uuid,
    attempt_count integer NOT NULL DEFAULT 1 CHECK (attempt_count > 0),
    lease_started_at timestamptz(6),
    lease_expires_at timestamptz(6),
    resulting_report_id uuid,
    error_detail text,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, shift_id, idempotency_key),
    FOREIGN KEY (org_id, station_id, shift_id)
        REFERENCES shifts (org_id, station_id, shift_id)
);
