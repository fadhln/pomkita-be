-- Reverse the BA2 station profile field.

ALTER TABLE stations
    DROP COLUMN IF EXISTS name;
