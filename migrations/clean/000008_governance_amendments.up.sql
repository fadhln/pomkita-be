-- Phase 4 amendment tables. Amendment decisions stay in Go services.

CREATE TABLE amendments (
    amendment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    base_report_id uuid NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'superseded')),
    requester_user_id uuid NOT NULL,
    approver_user_id uuid,
    requested_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    decided_at timestamptz(6),
    rejection_reason text,
    applied_report_id uuid,
    stale_check_hash bytea NOT NULL CHECK (octet_length(stale_check_hash) = 32),
    is_break_glass boolean NOT NULL DEFAULT false,
    break_glass_reason text,
    UNIQUE (org_id, station_id, amendment_id),
    FOREIGN KEY (org_id, station_id, shift_id, base_report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, station_id, shift_id, applied_report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    FOREIGN KEY (org_id, requester_user_id) REFERENCES users (org_id, user_id),
    FOREIGN KEY (org_id, approver_user_id) REFERENCES users (org_id, user_id),
    CHECK ((is_break_glass AND btrim(coalesce(break_glass_reason, '')) <> '')
        OR (NOT is_break_glass AND break_glass_reason IS NULL)),
    CHECK ((status = 'pending' AND approver_user_id IS NULL AND decided_at IS NULL
            AND rejection_reason IS NULL AND applied_report_id IS NULL)
        OR (status = 'approved' AND approver_user_id IS NOT NULL AND decided_at IS NOT NULL
            AND rejection_reason IS NULL AND applied_report_id IS NOT NULL)
        OR (status = 'rejected' AND approver_user_id IS NOT NULL AND decided_at IS NOT NULL
            AND btrim(coalesce(rejection_reason, '')) <> '' AND applied_report_id IS NULL)
        OR (status = 'superseded' AND decided_at IS NOT NULL))
);

CREATE UNIQUE INDEX amendments_one_pending_base
    ON amendments (org_id, station_id, base_report_id)
    WHERE status = 'pending';

CREATE TABLE amendment_items (
    item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    amendment_id uuid NOT NULL,
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('sales_declared', 'loss_entry', 'delivery', 'dip_reading')),
    target_logical_id uuid NOT NULL,
    field text NOT NULL CHECK (btrim(field) <> ''),
    old_value jsonb NOT NULL,
    new_value jsonb,
    UNIQUE (org_id, station_id, amendment_id, target_kind, target_logical_id, field),
    FOREIGN KEY (org_id, station_id, amendment_id)
        REFERENCES amendments (org_id, station_id, amendment_id)
);
