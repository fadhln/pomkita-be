-- Phase 1 catalog tables. The station is the lock scope for catalog writes.

SELECT pg_advisory_xact_lock(hashtext('pomkita:extension:btree_gist'));
CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;

CREATE TABLE dispensers (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    dispenser_id uuid NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (org_id, station_id, dispenser_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id)
);

CREATE TABLE tanks (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    tank_id uuid NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (org_id, station_id, tank_id),
    FOREIGN KEY (org_id, station_id) REFERENCES stations (org_id, station_id)
);

CREATE TABLE nozzles (
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL DEFAULT gen_random_uuid(),
    dispenser_id uuid NOT NULL,
    meter_max numeric(10,1) NOT NULL CHECK (meter_max >= 0),
    PRIMARY KEY (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, dispenser_id)
        REFERENCES dispensers (org_id, station_id, dispenser_id)
);

CREATE TABLE nozzle_tank_map (
    map_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    tank_id uuid NOT NULL,
    valid_period tstzrange NOT NULL,
    CHECK (NOT isempty(valid_period)),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, station_id, tank_id)
        REFERENCES tanks (org_id, station_id, tank_id),
    EXCLUDE USING gist (
        org_id WITH =,
        station_id WITH =,
        nozzle_id WITH =,
        valid_period WITH &&
    )
);

CREATE TABLE dispenser_nozzle_map (
    map_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    dispenser_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    valid_period tstzrange NOT NULL,
    CHECK (NOT isempty(valid_period)),
    FOREIGN KEY (org_id, station_id, dispenser_id)
        REFERENCES dispensers (org_id, station_id, dispenser_id),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    EXCLUDE USING gist (
        org_id WITH =,
        station_id WITH =,
        dispenser_id WITH =,
        valid_period WITH &&
    ),
    EXCLUDE USING gist (
        org_id WITH =,
        station_id WITH =,
        nozzle_id WITH =,
        valid_period WITH &&
    )
);

CREATE TABLE dispenser_prices (
    price_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,
    station_id uuid NOT NULL,
    nozzle_id uuid NOT NULL,
    price numeric(14,0) NOT NULL CHECK (price >= 0),
    valid_period tstzrange NOT NULL,
    created_by uuid NOT NULL,
    CHECK (NOT isempty(valid_period)),
    FOREIGN KEY (org_id, station_id, nozzle_id)
        REFERENCES nozzles (org_id, station_id, nozzle_id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, user_id),
    EXCLUDE USING gist (
        org_id WITH =,
        station_id WITH =,
        nozzle_id WITH =,
        valid_period WITH &&
    )
);
