-- PLAN.md sections 3.1, 4.2, 4.3, 5.1, 5.2, 7, and 8; BE-PLAN.md Phase B1.

do $$
begin
  if to_regclass('public.procedure_registry') is not null then
    delete from public.procedure_registry where name in ('fn_open_shift', 'fn_transition_shift', 'trg_shift_guard');
  end if;
end
$$;
drop trigger if exists shift_transitions_insert_guard on public.shift_transitions;
drop trigger if exists shifts_guard on public.shifts;
drop function if exists public.trg_shift_guard();
drop function if exists public.fn_transition_shift(uuid, public.shift_status, text);
drop function if exists public.fn_open_shift(uuid, uuid, timestamptz, boolean, date, integer, uuid, text);
alter table if exists public.meter_reset_events drop constraint if exists meter_reset_events_effective_shift_fk;
drop table if exists public.submit_idempotency;
drop table if exists public.draft_evidence_staging;
drop table if exists public.draft_losses;
drop table if exists public.draft_sales;
drop table if exists public.draft_readings;
drop table if exists public.shift_drafts;
drop table if exists public.shift_transitions;
drop table if exists public.shifts;
drop type if exists public.draft_status;
drop type if exists public.shift_status;
