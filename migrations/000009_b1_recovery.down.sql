-- PLAN.md sections 3.1, 4.2, 7, and 8; BE-PLAN.md Phase B1.
do $$ begin if to_regclass('public.procedure_registry') is not null then delete from public.procedure_registry where name in('fn_recover_submitting_shifts','read_shift_list','read_shift_detail','read_report');end if;end $$;
drop policy if exists recovery_job_idempotency on public.submit_idempotency;
drop policy if exists recovery_job_drafts on public.shift_drafts;
drop policy if exists recovery_job_shifts on public.shifts;
drop function if exists public.read_report(uuid);
drop function if exists public.read_shift_detail(uuid);
drop function if exists public.read_shift_list(uuid);
drop function if exists public.fn_recover_submitting_shifts();
