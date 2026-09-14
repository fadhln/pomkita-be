-- Phase 1 clean schema. Keep business decisions in Go services.
-- This migration contains tables and constraints only.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizations (
    org_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE stations (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL DEFAULT gen_random_uuid(),
    timezone text NOT NULL CHECK (btrim(timezone) <> ''),
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (org_id, station_id),
    FOREIGN KEY (org_id) REFERENCES organizations (org_id)
);

CREATE TABLE users (
    user_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL REFERENCES organizations (org_id),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, user_id)
);

CREATE TABLE user_station_roles (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('Operator', 'Supervisor', 'Station Admin', 'Owner', 'Superadmin')),
    PRIMARY KEY (org_id, station_id, user_id, role),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, user_id)
);

CREATE TABLE policy_snapshot_sets (
    set_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, set_id),
    UNIQUE (org_id, station_id, shift_id),
    UNIQUE (org_id, station_id, shift_id, set_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id)
);

CREATE TABLE shifts (
    shift_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    station_seq bigint NOT NULL CHECK (station_seq > 0),
    supervisor_id uuid NOT NULL,
    opened_at timestamptz(6) NOT NULL,
    closed_at timestamptz(6),
    timezone_snapshot text NOT NULL CHECK (btrim(timezone_snapshot) <> ''),
    business_date date NOT NULL,
    status text NOT NULL CHECK (status IN ('open', 'submitting', 'failed', 'abandoned', 'awaiting_confirmation', 'needs_correction', 'locked')),
    current_report_id uuid,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, shift_id),
    UNIQUE (org_id, station_id, station_seq),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (org_id, supervisor_id) REFERENCES users (org_id, user_id)
);

CREATE UNIQUE INDEX shifts_one_active_per_station
    ON shifts (org_id, station_id)
    WHERE status IN ('open', 'submitting', 'failed', 'awaiting_confirmation');

CREATE TABLE shift_drafts (
    draft_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    owned_by uuid,
    claim_token uuid,
    claim_expires_at timestamptz(6),
    status text NOT NULL CHECK (status IN ('editing', 'submitting', 'submitted', 'failed', 'recovering')),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, shift_id),
    UNIQUE (org_id, station_id, draft_id),
    FOREIGN KEY (org_id, station_id, shift_id) REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, owned_by) REFERENCES users (org_id, user_id)
);

CREATE TABLE shift_reports (
    report_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    shift_id uuid NOT NULL,
    version_no integer NOT NULL CHECK (version_no > 0),
    supersedes_report_id uuid,
    status text NOT NULL CHECK (status IN ('submitted', 'locked')),
    submitted_by uuid NOT NULL,
    submitted_at timestamptz(6) NOT NULL,
    policy_snapshot_set_id uuid NOT NULL,
    UNIQUE (org_id, station_id, report_id),
    UNIQUE (org_id, station_id, shift_id, report_id),
    UNIQUE (org_id, station_id, shift_id, version_no),
    UNIQUE (org_id, station_id, shift_id, report_id, version_no),
    FOREIGN KEY (org_id, station_id, shift_id) REFERENCES shifts (org_id, station_id, shift_id),
    FOREIGN KEY (org_id, submitted_by) REFERENCES users (org_id, user_id),
    FOREIGN KEY (org_id, station_id, shift_id, policy_snapshot_set_id)
        REFERENCES policy_snapshot_sets (org_id, station_id, shift_id, set_id)
);

ALTER TABLE shifts
    ADD CONSTRAINT shifts_current_report_fk
    FOREIGN KEY (org_id, station_id, shift_id, current_report_id)
    REFERENCES shift_reports (org_id, station_id, shift_id, report_id);

ALTER TABLE shift_reports
    ADD CONSTRAINT shift_reports_supersedes_fk
    FOREIGN KEY (org_id, station_id, shift_id, supersedes_report_id)
    REFERENCES shift_reports (org_id, station_id, shift_id, report_id);
