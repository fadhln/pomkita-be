-- BA2 adds the station name required by the own account profile.

ALTER TABLE stations
    ADD COLUMN name text NOT NULL DEFAULT 'Station'
    CHECK (btrim(name) <> '');
