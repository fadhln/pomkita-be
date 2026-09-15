-- Phase 4 acknowledgement tables. Business decisions stay in Go services.

CREATE TYPE ack_decision AS ENUM ('acked', 'rejected');

CREATE TABLE ack_decisions (
    ack_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    version_no integer NOT NULL,
    ack_seq bigint NOT NULL CHECK (ack_seq > 0),
    decision ack_decision NOT NULL,
    actor_user_id uuid NOT NULL,
    decided_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    rejection_reason text,
    is_superadmin boolean NOT NULL DEFAULT false,
    is_break_glass boolean NOT NULL DEFAULT false,
    break_glass_reason text,
    UNIQUE (org_id, station_id, shift_id, report_id, version_no, ack_id),
    UNIQUE (org_id, station_id, shift_id, report_id, version_no, ack_seq),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, version_no)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id, version_no),
    FOREIGN KEY (org_id, actor_user_id) REFERENCES users (org_id, user_id),
    CHECK ((decision = 'rejected' AND btrim(coalesce(rejection_reason, '')) <> '')
        OR (decision = 'acked' AND rejection_reason IS NULL)),
    CHECK ((is_break_glass AND btrim(coalesce(break_glass_reason, '')) <> '')
        OR (NOT is_break_glass AND break_glass_reason IS NULL))
);

CREATE TABLE ack_head (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    report_id uuid NOT NULL,
    version_no integer NOT NULL,
    active_ack_id uuid,
    PRIMARY KEY (org_id, station_id, shift_id, report_id, version_no),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, version_no)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id, version_no),
    FOREIGN KEY (org_id, station_id, shift_id, report_id, version_no, active_ack_id)
        REFERENCES ack_decisions (org_id, station_id, shift_id, report_id, version_no, ack_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE ack_supersessions (
    supersession_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    old_org_id uuid NOT NULL,
    old_station_id uuid NOT NULL,
    old_shift_id uuid NOT NULL,
    old_report_id uuid NOT NULL,
    old_version_no integer NOT NULL,
    superseded_ack_id uuid NOT NULL,
    replacement_org_id uuid NOT NULL,
    replacement_station_id uuid NOT NULL,
    replacement_shift_id uuid NOT NULL,
    replacement_report_id uuid NOT NULL,
    replacement_version_no integer NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id),
    FOREIGN KEY (old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id)
        REFERENCES ack_decisions (org_id, station_id, shift_id, report_id, version_no, ack_id),
    FOREIGN KEY (replacement_org_id, replacement_station_id, replacement_shift_id, replacement_report_id)
        REFERENCES shift_reports (org_id, station_id, shift_id, report_id),
    CHECK (replacement_version_no = old_version_no + 1)
);

