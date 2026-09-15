-- BA3b adds the station administration fields.

ALTER TABLE stations
    ADD COLUMN code text,
    ADD COLUMN address text,
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN updated_at timestamptz(6);

CREATE UNIQUE INDEX stations_org_code_ci ON stations (org_id, lower(code));

ALTER TABLE stations
    ALTER COLUMN name DROP DEFAULT;
