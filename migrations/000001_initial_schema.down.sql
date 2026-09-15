-- Phase 1 clean schema rollback.

ALTER TABLE IF EXISTS shifts DROP CONSTRAINT IF EXISTS shifts_current_report_fk;
DROP TABLE IF EXISTS shift_reports;
DROP TABLE IF EXISTS shift_drafts;
DROP TABLE IF EXISTS shifts;
DROP TABLE IF EXISTS policy_snapshot_sets;
DROP TABLE IF EXISTS user_station_roles;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS stations;
DROP TABLE IF EXISTS organizations;
