-- Phase 1 report input rollback.

ALTER TABLE IF EXISTS submit_idempotency
    DROP CONSTRAINT IF EXISTS submit_idempotency_report_fk;
DROP TABLE IF EXISTS meter_reset_events;
DROP TABLE IF EXISTS shift_transitions;
DROP TABLE IF EXISTS dip_readings;
DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS loss_entries;
DROP TABLE IF EXISTS loss_identity;
DROP TABLE IF EXISTS sales_declared;
DROP TABLE IF EXISTS dispenser_readings;
