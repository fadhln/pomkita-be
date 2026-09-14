-- PLAN.md sections 4.1, 4.3, 5.2, 7, and 8; BE-PLAN.md Phase B1.

drop table if exists public.nozzle_baseline_current;
drop table if exists public.nozzle_baseline_revisions;
drop table if exists public.meter_reset_events;
drop table if exists public.dispenser_prices;
drop table if exists public.dispenser_nozzle_map;
drop table if exists public.nozzle_tank_map;
drop table if exists public.nozzles;
drop table if exists public.tanks;
drop table if exists public.dispensers;
-- Keep the shared extension. Other concurrent schemas can still use it.
