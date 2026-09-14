# Clean migration source

This directory contains the Phase 1 and Phase 2 table-only migration sequence.

The sequence has reversible up and down files. It uses PostgreSQL constraints,
indexes, numeric columns, and exclusion constraints. It does not create
business functions, procedures, triggers, roles, or grants.

The sequence is available through `internal/platform/migrations`. Production
uses this source with the GORM repositories and Go service boundaries.
