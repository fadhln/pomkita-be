-- Store one active organization and station for each login session.
CREATE TABLE session_active_context (
    jti uuid PRIMARY KEY REFERENCES sessions (jti) ON DELETE CASCADE,
    org_id uuid NOT NULL REFERENCES organizations (org_id),
    station_id uuid NOT NULL,
    updated_at timestamptz(6) NOT NULL,
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id)
);
