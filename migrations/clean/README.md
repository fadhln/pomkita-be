# Clean migration source

This directory contains the Phase 1 table-only migration sequence.

The sequence has reversible up and down files. It uses PostgreSQL constraints,
indexes, numeric columns, and exclusion constraints. It does not create
business functions, procedures, triggers, roles, or grants.

The sequence is available through `internal/platform/migrations`. The legacy
server still uses the original migration directory until the GORM repositories
and service wiring are ready in the next phase.
