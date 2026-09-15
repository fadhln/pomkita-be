-- BA3 organization detail fields.

ALTER TABLE organizations
    ADD COLUMN legal_name text,
    ADD COLUMN address text,
    ADD COLUMN contact_email text,
    ADD COLUMN timezone text,
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN updated_at timestamptz(6);
