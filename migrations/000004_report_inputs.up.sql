-- Phase 1 report input tables. These tables store immutable submit inputs.

CREATE TABLE dispenser_readings (
    reading_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    meter_start numeric(10,1) NOT NULL CHECK (meter_start >= 0),
    meter_end numeric(10,1) NOT NULL CHECK (meter_end >= 0),
    price_used numeric(14,0) NOT NULL CHECK (price_used >= 0),
    expected_sale_rupiah numeric(14,0) NOT NULL CHECK (expected_sale_rupiah >= 0),
    observed boolean NOT NULL,
    is_carried_forward boolean NOT NULL,
    source_shift_id uuid,
    source_report_id uuid,
    source_reading_id uuid,
    UNIQUE (org_id, station_id, shift_id, report_id, nozzle_id),
    UNIQUE (org_id, station_id, shift_id, report_id, reading_id),
    CHECK ((is_carried_forward AND source_shift_id IS NOT NULL AND source_report_id IS NOT NULL AND source_reading_id IS NOT NULL)
        OR (NOT is_carried_forward AND source_shift_id IS NULL AND source_report_id IS NULL AND source_reading_id IS NULL)),
    FOREIGN KEY (org_id, station_id, shift_id, report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, source_shift_id, source_report_id, source_reading_id)
        REFERENCES dispenser_readings (org_id, station_id, shift_id, report_id, reading_id)
);

CREATE TABLE sales_declared (
    sales_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    dispenser_id uuid NOT NULL,
    cash_amount numeric(14,0) NOT NULL CHECK (cash_amount >= 0),
    cashless_amount numeric(14,0) NOT NULL DEFAULT 0 CHECK (cashless_amount >= 0),
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, report_id, dispenser_id),
    UNIQUE (org_id, station_id, shift_id, report_id, sales_id),
    FOREIGN KEY (org_id, station_id, shift_id, report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, station_id, dispenser_id)
        REFERENCES dispensers (org_id, station_id, dispenser_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE loss_identity (
    loss_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, loss_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE loss_entries (
    row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    version_no integer NOT NULL,
    loss_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    direction text NOT NULL CHECK (direction IN ('loss', 'gain')),
    reason_code text NOT NULL CHECK (btrim(reason_code) <> ''),
    liters numeric(8,2) NOT NULL CHECK (liters >= 0),
    cash_amount numeric(14,0) CHECK (cash_amount >= 0),
    note text,
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, report_id, loss_id),
    UNIQUE (org_id, station_id, shift_id, report_id, row_id),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, version_no)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id, version_no),
    FOREIGN KEY (org_id, station_id, loss_id)
        REFERENCES loss_identity (org_id, station_id, loss_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE deliveries (
    delivery_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    do_number text NOT NULL CHECK (btrim(do_number) <> ''),
    tank_id uuid NOT NULL,
    liters numeric(8,2) NOT NULL CHECK (liters >= 0),
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, delivery_id),
    FOREIGN KEY (org_id, station_id, shift_id)
        REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, station_id, tank_id)
        REFERENCES tanks (org_id, station_id, tank_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE dip_readings (
    dip_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    tank_id uuid NOT NULL,
    dip_liters numeric(8,2) NOT NULL CHECK (dip_liters >= 0),
    created_by uuid NOT NULL,
    UNIQUE (org_id, station_id, shift_id, dip_id),
    FOREIGN KEY (org_id, station_id, shift_id)
        REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, station_id, tank_id)
        REFERENCES tanks (org_id, station_id, tank_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE shift_transitions (
    transition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    from_status text NOT NULL,
    to_status text NOT NULL,
    actor_user_id uuid NOT NULL,
    at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    reason text,
    FOREIGN KEY (org_id, station_id, shift_id)
        REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, actor_user_id) REFERENCES users (org_id, user_id)
);

CREATE TABLE meter_reset_events (
    reset_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    old_value numeric(10,1) NOT NULL CHECK (old_value >= 0),
    new_value numeric(10,1) NOT NULL CHECK (new_value >= 0),
    effective_shift_id uuid NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    actor_user_id uuid NOT NULL,
    approver_user_id uuid NOT NULL,
    approved_at timestamptz(6),
    status text NOT NULL CHECK (status IN ('pending', 'approved')),
    UNIQUE (org_id, station_id, reset_id),
    CHECK (actor_user_id <> approver_user_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, effective_shift_id)
        REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, actor_user_id) REFERENCES users (org_id, user_id),
    FOREIGN KEY (org_id, approver_user_id) REFERENCES users (org_id, user_id)
);

CREATE UNIQUE INDEX meter_reset_events_one_approved
    ON meter_reset_events (org_id, station_id, nozzle_id, effective_shift_id)
    WHERE status = 'approved';

ALTER TABLE submit_idempotency
    ADD CONSTRAINT submit_idempotency_report_fk
    FOREIGN KEY (org_id, station_id, shift_id, resulting_report_id)
    REFERENCES shift_reports (org_id, station_id, shift_id, report_id);
