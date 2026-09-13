-- PLAN.md sections 3.1, 7, and 8; BE-PLAN.md Phase B2.

delete from public.procedure_registry where name in ('fn_abandon_failed_shifts', 'read_governance_anomalies');
drop function if exists public.read_governance_anomalies();
drop function if exists public.fn_abandon_failed_shifts();
