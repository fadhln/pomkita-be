-- Reverse the BA3b station administration fields.

DROP INDEX IF EXISTS stations_org_code_ci;

ALTER TABLE stations
    ALTER COLUMN name SET DEFAULT 'Station',
    DROP COLUMN IF EXISTS code,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS updated_at;
