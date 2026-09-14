-- Phase 1 policy snapshots, report snapshots, evidence, and meter baselines.

ALTER TABLE shifts
    ADD COLUMN original_event_date date,
    ADD COLUMN shift_ke integer CHECK (shift_ke > 0),
    ADD COLUMN backfilled boolean NOT NULL DEFAULT false,
    ADD COLUMN backfill_approver uuid,
    ADD COLUMN backfill_approved_at timestamptz(6),
    ADD COLUMN backfill_reason text,
    ADD COLUMN shift_price_map_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN shift_price_map_hash bytea NOT NULL DEFAULT decode(repeat('00', 32), 'hex'),
    ADD CONSTRAINT shifts_price_map_hash_length CHECK (octet_length(shift_price_map_hash) = 32),
    ADD CONSTRAINT shifts_backfill_fields CHECK (
        (NOT backfilled)
        OR (original_event_date IS NOT NULL AND shift_ke IS NOT NULL AND backfill_approver IS NOT NULL
            AND backfill_approved_at IS NOT NULL AND btrim(coalesce(backfill_reason, '')) <> '')
    ),
    ADD FOREIGN KEY (org_id, backfill_approver) REFERENCES users (org_id, user_id);

CREATE UNIQUE INDEX shifts_backfill_key
    ON shifts (org_id, station_id, original_event_date, shift_ke)
    WHERE backfilled;

ALTER TABLE shift_drafts
    ADD COLUMN updated_by uuid,
    ADD COLUMN recovery_count integer NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
    ADD FOREIGN KEY (org_id, updated_by) REFERENCES users (org_id, user_id);

ALTER TABLE policy_snapshot_sets
    ADD CONSTRAINT policy_snapshot_sets_shift_fk
    FOREIGN KEY (org_id, station_id, shift_id)
    REFERENCES shifts (org_id, station_id, shift_id);

CREATE TABLE threshold_policy_revisions (
    rev_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id uuid NOT NULL,
    org_id uuid NOT NULL,
    station_id uuid,
    valid_from timestamptz(6) NOT NULL,
    supersedes_org_id uuid,
    supersedes_rev_id uuid,
    disabled boolean NOT NULL DEFAULT false,
    loss_liter_threshold numeric(8,2) NOT NULL CHECK (loss_liter_threshold >= 0),
    gain_liter_threshold numeric(8,2) NOT NULL CHECK (gain_liter_threshold >= 0),
    loss_rupiah_threshold numeric(14,0) NOT NULL CHECK (loss_rupiah_threshold >= 0),
    gain_rupiah_threshold numeric(14,0) NOT NULL CHECK (gain_rupiah_threshold >= 0),
    variance_rupiah_threshold numeric(14,0) NOT NULL CHECK (variance_rupiah_threshold >= 0),
    rollover_threshold numeric(10,1) NOT NULL CHECK (rollover_threshold >= 0),
    created_by uuid NOT NULL,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, rev_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (supersedes_org_id, supersedes_rev_id)
        REFERENCES threshold_policy_revisions (org_id, rev_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE evidence_policy_revisions (
    rev_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id uuid NOT NULL,
    org_id uuid NOT NULL,
    station_id uuid,
    valid_from timestamptz(6) NOT NULL,
    supersedes_org_id uuid,
    supersedes_rev_id uuid,
    mode text NOT NULL CHECK (mode IN ('opsional', 'wajib')),
    disabled boolean NOT NULL DEFAULT false,
    created_by uuid NOT NULL,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, rev_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (supersedes_org_id, supersedes_rev_id)
        REFERENCES evidence_policy_revisions (org_id, rev_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE evidence_policy_types (
    org_id uuid NOT NULL,
    rev_id uuid NOT NULL,
    evidence_type text NOT NULL CHECK (btrim(evidence_type) <> ''),
    minimum_count_per_loss integer NOT NULL CHECK (minimum_count_per_loss >= 0),
    accepted_mime_types text[] NOT NULL CHECK (cardinality(accepted_mime_types) > 0),
    PRIMARY KEY (org_id, rev_id, evidence_type),
    FOREIGN KEY (org_id, rev_id)
        REFERENCES evidence_policy_revisions (org_id, rev_id)
);

CREATE TABLE policy_snapshot_items (
    item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    set_id uuid NOT NULL,
    policy_kind text NOT NULL CHECK (policy_kind IN ('threshold', 'evidence')),
    policy_id uuid NOT NULL,
    rev_id uuid NOT NULL,
    scope text NOT NULL CHECK (scope IN ('organization', 'station')),
    payload jsonb NOT NULL,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    UNIQUE (org_id, station_id, shift_id, set_id, policy_kind),
    FOREIGN KEY (org_id, station_id, shift_id, set_id)
        REFERENCES policy_snapshot_sets (org_id, station_id, shift_id, set_id)
);

CREATE TABLE delivery_snapshots (
    snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    delivery_id uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, report_id, delivery_id),
    FOREIGN KEY (org_id, station_id, shift_id, report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, station_id, shift_id, delivery_id)
        REFERENCES deliveries (org_id, station_id, shift_id, delivery_id)
);

CREATE TABLE dip_snapshots (
    snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    dip_id uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, report_id, dip_id),
    FOREIGN KEY (org_id, station_id, shift_id, report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, station_id, shift_id, dip_id)
        REFERENCES dip_readings (org_id, station_id, shift_id, dip_id)
);

CREATE TABLE loss_exception (
    exception_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    loss_id uuid NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    actor_user_id uuid NOT NULL,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, shift_id, report_id, loss_id),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, loss_id)
        REFERENCES loss_entries (org_id, station_id, shift_id, report_id, loss_id),
    FOREIGN KEY (org_id, actor_user_id) REFERENCES users (org_id, user_id)
);

CREATE TABLE evidence_event (
    evidence_event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evidence_id uuid NOT NULL,
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    loss_row_id uuid NOT NULL,
    event_seq bigint NOT NULL CHECK (event_seq >= 1),
    event_type text NOT NULL CHECK (event_type IN ('uploaded', 'verified', 'finalized')),
    evidence_type text NOT NULL,
    object_key text NOT NULL CHECK (btrim(object_key) <> ''),
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    mime text NOT NULL CHECK (btrim(mime) <> ''),
    actor_user_id uuid NOT NULL,
    at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, shift_id, report_id, loss_row_id, evidence_id, event_seq),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, loss_row_id)
        REFERENCES loss_entries (org_id, station_id, shift_id, report_id, row_id),
    FOREIGN KEY (org_id, actor_user_id) REFERENCES users (org_id, user_id)
);

CREATE TABLE nozzle_baseline_revisions (
    baseline_rev_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    effective_station_seq bigint NOT NULL CHECK (effective_station_seq >= 0),
    initial_meter_value numeric(10,1) NOT NULL CHECK (initial_meter_value >= 0),
    meter_max numeric(10,1) NOT NULL CHECK (meter_max >= 0),
    created_by uuid NOT NULL,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, nozzle_id, baseline_rev_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE nozzle_baseline_current (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    current_baseline_rev_id uuid NOT NULL,
    PRIMARY KEY (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, nozzle_id, current_baseline_rev_id)
        REFERENCES nozzle_baseline_revisions (org_id, station_id, nozzle_id, baseline_rev_id)
);
