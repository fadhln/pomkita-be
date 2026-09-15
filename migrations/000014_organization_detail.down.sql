-- Reverse BA3 organization detail fields only.

ALTER TABLE organizations
    DROP COLUMN IF EXISTS legal_name,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS contact_email,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS updated_at;
