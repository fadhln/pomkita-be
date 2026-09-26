ALTER TABLE users
    ADD COLUMN preferred_org_id uuid NULL,
    ADD COLUMN preferred_station_id uuid NULL,
    ADD CONSTRAINT users_preferred_station_fk
        FOREIGN KEY (preferred_org_id, preferred_station_id)
        REFERENCES stations (org_id, station_id);
