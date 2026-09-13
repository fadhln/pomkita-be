-- PLAN.md sections 3.1, 7, and 8; BE-PLAN.md Phase B2.

create or replace function public.fn_abandon_failed_shifts()
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_shift record;
  v_count integer := 0;
begin
  for v_shift in
    select s.org_id, s.station_id, s.shift_id
      from public.shifts s
     where s.status = 'failed'
       and coalesce(s.closed_at, s.created_at) < clock_timestamp() - interval '24 hours'
     order by s.org_id, s.station_id, s.station_seq
  loop
    perform set_config('app.context_valid', 'true', true);
    perform set_config('app.org_id', v_shift.org_id::text, true);
    perform set_config('app.station_id', v_shift.station_id::text, true);
    perform set_config('app.user_id', '', true);
    perform set_config('app.role', '', true);
    perform public.fn_transition_shift(v_shift.shift_id, 'abandoned'::public.shift_status, 'scheduler');
    v_count := v_count + 1;
  end loop;
  return v_count;
end;
$$;
alter function public.fn_abandon_failed_shifts() owner to station_owner;

create or replace function public.read_governance_anomalies()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  return query
    select jsonb_build_object(
      'source', 'ack_decision', 'source_id', d.ack_id::text,
      'shift_id', d.shift_id::text, 'report_id', d.report_id::text,
      'version_no', d.version_no, 'is_break_glass', d.is_break_glass,
      'is_superadmin', d.is_superadmin, 'reason', d.break_glass_reason,
      'at', d.decided_at)
      from public.ack_decisions d
     where d.org_id = nullif(current_setting('app.org_id', true), '')::uuid
       and d.is_break_glass
    union all
    select jsonb_build_object(
      'source', 'amendment', 'source_id', a.amendment_id::text,
      'shift_id', a.shift_id::text, 'report_id', a.base_report_id::text,
      'is_break_glass', a.is_break_glass, 'reason', a.break_glass_reason,
      'at', coalesce(a.decided_at, a.requested_at))
      from public.amendments a
     where a.org_id = nullif(current_setting('app.org_id', true), '')::uuid
       and a.is_break_glass;
end;
$$;
alter function public.read_governance_anomalies() owner to report_writer;

grant execute on function public.fn_abandon_failed_shifts() to station_owner;
grant execute on function public.read_governance_anomalies() to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_abandon_failed_shifts', array[]::text[], 'abandon_failed_shifts', 20),
  ('read_governance_anomalies', array['Station Admin', 'Owner', 'Superadmin'], 'read_governance_anomalies', 53)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
