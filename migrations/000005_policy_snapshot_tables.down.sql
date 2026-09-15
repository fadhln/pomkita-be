-- Phase 1 policy and snapshot rollback.

DROP TABLE IF EXISTS nozzle_baseline_current;
DROP TABLE IF EXISTS nozzle_baseline_revisions;
DROP TABLE IF EXISTS evidence_event;
DROP TABLE IF EXISTS loss_exception;
DROP TABLE IF EXISTS dip_snapshots;
DROP TABLE IF EXISTS delivery_snapshots;
DROP TABLE IF EXISTS policy_snapshot_items;
DROP TABLE IF EXISTS evidence_policy_types;
DROP TABLE IF EXISTS evidence_policy_revisions;
DROP TABLE IF EXISTS threshold_policy_revisions;

ALTER TABLE policy_snapshot_sets
    DROP CONSTRAINT IF EXISTS policy_snapshot_sets_shift_fk;
ALTER TABLE shift_drafts
    DROP CONSTRAINT IF EXISTS shift_drafts_org_id_updated_by_fkey;
ALTER TABLE shift_drafts
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS recovery_count;
ALTER TABLE shifts
    DROP CONSTRAINT IF EXISTS shifts_backfill_fields,
    DROP CONSTRAINT IF EXISTS shifts_price_map_hash_length,
    DROP CONSTRAINT IF EXISTS shifts_org_id_backfill_approver_fkey;
DROP INDEX IF EXISTS shifts_backfill_key;
ALTER TABLE shifts
    DROP COLUMN IF EXISTS original_event_date,
    DROP COLUMN IF EXISTS shift_ke,
    DROP COLUMN IF EXISTS backfilled,
    DROP COLUMN IF EXISTS backfill_approver,
    DROP COLUMN IF EXISTS backfill_approved_at,
    DROP COLUMN IF EXISTS backfill_reason,
    DROP COLUMN IF EXISTS shift_price_map_snapshot,
    DROP COLUMN IF EXISTS shift_price_map_hash;
