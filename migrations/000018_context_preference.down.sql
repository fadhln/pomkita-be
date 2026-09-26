ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_preferred_station_fk,
    DROP COLUMN IF EXISTS preferred_station_id,
    DROP COLUMN IF EXISTS preferred_org_id;
