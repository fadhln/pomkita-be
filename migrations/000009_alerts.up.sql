-- Phase 4 alert tables. Alert evaluation and idempotency stay in Go services.

CREATE TYPE alert_rule_type AS ENUM ('starvation', 'variance');
CREATE TYPE alert_channel AS ENUM ('in_app');
CREATE TYPE alert_subject_kind AS ENUM ('shift', 'report');
CREATE TYPE alert_event_type AS ENUM ('fired', 'cleared');
CREATE TYPE alert_source_kind AS ENUM ('shift_transition', 'report', 'scheduler', 'amendment');

CREATE TABLE alert_rules (
    rule_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    rule_type alert_rule_type NOT NULL,
    alert_key text NOT NULL CHECK (btrim(alert_key) <> ''),
    threshold numeric NOT NULL CHECK (threshold >= 0),
    enabled boolean NOT NULL DEFAULT true,
    channel alert_channel NOT NULL DEFAULT 'in_app',
    created_by uuid,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, rule_id),
    UNIQUE (org_id, station_id, alert_key),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id),
    CHECK (rule_type <> 'starvation' OR threshold = 24)
);

CREATE TABLE alert_events (
    event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    subject_kind alert_subject_kind NOT NULL,
    subject_id uuid NOT NULL,
    event_type alert_event_type NOT NULL,
    period_start timestamptz(6) NOT NULL,
    period_bucket timestamptz(6) GENERATED ALWAYS AS
        (date_bin(interval '1 hour', period_start, timestamptz '1970-01-01 00:00:00+00')) STORED,
    related_fired_event_id uuid,
    source_kind alert_source_kind NOT NULL,
    source_id uuid NOT NULL,
    source_version_no integer,
    source_at timestamptz(6) NOT NULL,
    created_by uuid,
    created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (org_id, station_id, event_id),
    UNIQUE (org_id, station_id, rule_id, subject_kind, subject_id, event_type, period_bucket),
    UNIQUE (org_id, station_id, related_fired_event_id, event_type),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id),
    FOREIGN KEY (org_id, station_id, rule_id) REFERENCES alert_rules (org_id, station_id, rule_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id),
    FOREIGN KEY (org_id, station_id, related_fired_event_id)
        REFERENCES alert_events (org_id, station_id, event_id)
        DEFERRABLE INITIALLY DEFERRED,
    CHECK ((event_type = 'fired' AND related_fired_event_id = event_id)
        OR (event_type = 'cleared' AND related_fired_event_id IS NOT NULL)),
    CHECK ((source_kind IN ('scheduler', 'shift_transition') AND source_version_no IS NULL)
        OR (source_kind IN ('report', 'amendment') AND source_version_no IS NOT NULL)),
    CHECK (source_version_no IS NULL OR source_version_no > 0)
);
