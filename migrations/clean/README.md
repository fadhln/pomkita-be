# Clean migration source

This directory contains the Phase 1 and Phase 2 table-only migration sequence.

The sequence has reversible up and down files. It uses PostgreSQL constraints,
indexes, numeric columns, and exclusion constraints. It does not create
business functions, procedures, triggers, roles, or grants.

The sequence is available through `internal/platform/migrations`. The legacy
server still uses the original migration directory for the operational managers.
Authentication uses the GORM repository and Go service boundary.
