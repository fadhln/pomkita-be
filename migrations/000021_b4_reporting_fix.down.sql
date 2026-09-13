-- The next down migration removes the reporting function with migration 000020.
drop function if exists public.read_anomaly_export();
