# Seed data

Seed data is deterministic and safe to recreate in development and tests. It must never contain production credentials or secrets.

Apply `demo.sql` after all migrations. It creates one demo organization, one station, one dispenser, one nozzle, one tank, Supervisor, Station Admin, and Owner users, plus JWT key metadata, policy revisions, and the audit-chain lock.

The demo password is `demo-password`. The JWT secret is the development-only value `pomkita-demo-only-secret`; set it as `JWT_SECRET_KEY_1` when the server uses this seed.
