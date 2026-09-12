-- PLAN.md sections 3.2, 3.3, 4.2, 4.3, 7, and 8; BE-PLAN.md Phase B2.

create table public.amendments (
  amendment_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  base_report_id uuid not null,
  reason text not null,
  status text not null default 'pending',
  requester_user_id uuid not null,
  approver_user_id uuid,
  requested_at timestamptz(6) not null default clock_timestamp(),
  decided_at timestamptz(6),
  rejection_reason text,
  applied_report_id uuid,
  stale_check_hash bytea not null check (octet_length(stale_check_hash) = 32),
  is_break_glass boolean not null default false,
  break_glass_reason text,
  unique (org_id, station_id, amendment_id),
  foreign key (org_id, station_id, shift_id, base_report_id)
    references public.shift_reports (org_id, station_id, shift_id, report_id),
  foreign key (org_id, station_id, shift_id, applied_report_id)
    references public.shift_reports (org_id, station_id, shift_id, report_id),
  foreign key (org_id, requester_user_id) references public.users (org_id, user_id),
  foreign key (org_id, approver_user_id) references public.users (org_id, user_id),
  check (status in ('pending', 'approved', 'rejected', 'superseded')),
  check (btrim(reason) <> ''),
  check ((is_break_glass and btrim(coalesce(break_glass_reason, '')) <> '')
      or (not is_break_glass and break_glass_reason is null)),
  check ((status = 'pending' and approver_user_id is null and decided_at is null
          and rejection_reason is null and applied_report_id is null)
      or (status = 'approved' and approver_user_id is not null and decided_at is not null
          and rejection_reason is null and applied_report_id is not null)
      or (status = 'rejected' and approver_user_id is not null and decided_at is not null
          and btrim(coalesce(rejection_reason, '')) <> '' and applied_report_id is null)
      or (status = 'superseded' and decided_at is not null))
);
alter table public.amendments owner to report_writer;
create unique index amendments_one_pending_base
  on public.amendments (org_id, station_id, base_report_id)
  where status = 'pending';

create table public.amendment_items (
  item_id uuid primary key default app.gen_random_uuid(),
  amendment_id uuid not null,
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  target_kind text not null,
  target_logical_id uuid not null,
  field text not null,
  old_value jsonb not null,
  new_value jsonb,
  unique (org_id, station_id, amendment_id, target_kind, target_logical_id, field),
  foreign key (org_id, station_id, amendment_id)
    references public.amendments (org_id, station_id, amendment_id),
  check (target_kind in ('sales_declared', 'loss_entry', 'delivery', 'dip_reading')),
  check (btrim(field) <> '')
);
alter table public.amendment_items owner to report_writer;

create or replace function public.trg_amendment_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op in ('UPDATE', 'DELETE')
     and current_setting('app.transition', true) <> 'approve_amendment' then
    raise exception using errcode = '23514', message = 'amendment_write_forbidden';
  end if;
  return coalesce(new, old);
end;
$$;
alter function public.trg_amendment_guard() owner to report_writer;
create trigger amendments_guard before update or delete on public.amendments
for each row execute function public.trg_amendment_guard();
create trigger amendment_items_guard before update or delete on public.amendment_items
for each row execute function public.trg_amendment_guard();

create or replace function public.trg_ack_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_table_name = 'ack_decisions' then
    if tg_op <> 'INSERT' then
      raise exception using errcode = '23514', message = 'ack_decision_immutable';
    end if;
    return new;
  end if;
  if tg_table_name = 'ack_supersessions' then
    if tg_op <> 'INSERT' or current_setting('app.transition', true) <> 'approve_amendment' then
      raise exception using errcode = '23514', message = 'ack_supersession_write_forbidden';
    end if;
    return new;
  end if;
  if tg_op = 'DELETE' then
    raise exception using errcode = '23514', message = 'ack_head_delete_forbidden';
  end if;
  if tg_op = 'INSERT' then
    if current_setting('app.transition', true) not in ('submit_shift', 'approve_amendment') then
      raise exception using errcode = '23514', message = 'ack_head_insert_forbidden';
    end if;
    return new;
  end if;
  if current_setting('app.transition', true) not in ('ack_shift', 'approve_amendment')
     or row_to_json(new)::jsonb - 'active_ack_id' <> row_to_json(old)::jsonb - 'active_ack_id'
  then
    raise exception using errcode = '23514', message = 'ack_head_update_forbidden';
  end if;
  return new;
end;
$$;
alter function public.trg_ack_guard() owner to report_writer;

create or replace function public.trg_shift_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op = 'DELETE' then
    raise exception using errcode = '23514', message = 'shift_delete_forbidden';
  end if;
  if tg_table_name = 'shift_transitions' then
    if current_setting('app.transition', true) not in ('shift_transition', 'submit_shift', 'recovery') then
      raise exception using errcode = '23514', message = 'transition_insert_forbidden';
    end if;
    return new;
  end if;
  if new.station_seq <> old.station_seq
     or new.shift_price_map_snapshot <> old.shift_price_map_snapshot
     or new.shift_price_map_hash <> old.shift_price_map_hash then
    raise exception using errcode = '23514', message = 'shift_immutable_fields';
  end if;
  if new.current_report_id is distinct from old.current_report_id
     and current_setting('app.transition', true) not in ('submit_shift', 'approve_amendment') then
    raise exception using errcode = '23514', message = 'current_report_pointer_forbidden';
  end if;
  if new.status <> old.status
     and current_setting('app.transition', true) not in ('shift_transition', 'submit_shift', 'recovery', 'ack_shift') then
    raise exception using errcode = '23514', message = 'shift_transition_required';
  end if;
  return new;
end;
$$;
alter function public.trg_shift_guard() owner to station_owner;

create or replace function public.fn_amendment_base_payload(
  p_org_id uuid, p_station_id uuid, p_shift_id uuid, p_report_id uuid
)
returns jsonb
language sql
security definer
set search_path = pg_catalog, public, app
as $function$
  select jsonb_build_object(
    'report_id', r.report_id::text,
    'shift_id', r.shift_id::text,
    'version_no', r.version_no,
    'status', r.status,
    'submitted_at', r.submitted_at,
    'readings', coalesce((select jsonb_agg(jsonb_build_object(
      'nozzle_id', x.nozzle_id::text, 'meter_start', x.meter_start::text,
      'meter_end', x.meter_end::text, 'price_used', x.price_used::text,
      'expected_sale_rupiah', x.expected_sale_rupiah::text) order by x.nozzle_id)
      from public.dispenser_readings x where x.org_id = r.org_id and x.station_id = r.station_id and x.report_id = r.report_id), '[]'::jsonb),
    'sales', coalesce((select jsonb_agg(jsonb_build_object(
      'dispenser_id', x.dispenser_id::text, 'cash_amount', x.cash_amount::text,
      'cashless_amount', x.cashless_amount::text) order by x.dispenser_id)
      from public.sales_declared x where x.org_id = r.org_id and x.station_id = r.station_id and x.report_id = r.report_id), '[]'::jsonb),
    'losses', coalesce((select jsonb_agg(jsonb_build_object(
      'loss_id', x.loss_id::text, 'direction', x.direction, 'liters', x.liters::text,
      'cash_amount', x.cash_amount::text, 'note', x.note) order by x.row_id)
      from public.loss_entries x where x.org_id = r.org_id and x.station_id = r.station_id and x.report_id = r.report_id), '[]'::jsonb)
  )
    from public.shift_reports r
   where r.org_id = p_org_id and r.station_id = p_station_id
     and r.shift_id = p_shift_id and r.report_id = p_report_id;
$function$;
alter function public.fn_amendment_base_payload(uuid, uuid, uuid, uuid) owner to report_writer;

create or replace function public.fn_approve_amendment(
  p_amendment_id uuid,
  p_stale_check_hash bytea
)
returns table (amendment_id uuid, applied_report_id uuid, version_no integer)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_station_id uuid;
  v_shift_id uuid;
  v_base_report_id uuid;
  v_base_version integer;
  v_shift_status text;
  v_current_report uuid;
  v_status text;
  v_base_status text;
  v_requester uuid;
  v_break_glass boolean;
  v_break_glass_reason text;
  v_payload jsonb;
  v_expected_hash bytea;
  v_new_report uuid := app.gen_random_uuid();
  v_new_version integer;
  v_old_head public.ack_head%rowtype;
  v_item record;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'amendment_role_required';
  end if;
  select a.station_id, a.shift_id, a.base_report_id, a.requester_user_id,
         a.is_break_glass, a.break_glass_reason, a.status,
         s.current_report_id, s.status, r.version_no, r.status
    into v_station_id, v_shift_id, v_base_report_id, v_requester,
         v_break_glass, v_break_glass_reason, v_status,
         v_current_report, v_shift_status, v_base_version, v_base_status
    from public.amendments a
    join public.shifts s on s.org_id = a.org_id and s.station_id = a.station_id and s.shift_id = a.shift_id
    join public.shift_reports r on r.org_id = a.org_id and r.station_id = a.station_id and r.shift_id = a.shift_id and r.report_id = a.base_report_id
   where a.org_id = v_org_id and a.amendment_id = p_amendment_id
   for update of a, s, r;
  if not found then
    raise exception using errcode = '42501', message = 'amendment_not_found';
  end if;
  if v_status <> 'pending' then
    raise exception using errcode = '23505', message = 'amendment_not_pending';
  end if;
  perform 1 from public.stations where org_id = v_org_id and station_id = v_station_id for update;
  if v_current_report <> v_base_report_id or v_base_status not in ('submitted', 'locked') then
    raise exception using errcode = '23505', message = 'amendment_base_not_current';
  end if;
  if p_stale_check_hash is null or octet_length(p_stale_check_hash) <> 32 then
    raise exception using errcode = '23505', message = 'stale_amendment_base';
  end if;
  v_payload := public.fn_amendment_base_payload(v_org_id, v_station_id, v_shift_id, v_base_report_id);
  v_expected_hash := app.digest(convert_to(jsonb_build_array(v_payload, v_base_version)::text, 'UTF8'), 'sha256');
  if p_stale_check_hash <> v_expected_hash then
    raise exception using errcode = '23505', message = 'stale_amendment_base';
  end if;
  if v_actor = v_requester and not v_break_glass then
    raise exception using errcode = '42501', message = 'amendment_separation_required';
  end if;
  if v_break_glass and (v_role not in ('Owner', 'Superadmin')
      or btrim(coalesce(v_break_glass_reason, '')) = '') then
    raise exception using errcode = '23514', message = 'break_glass_reason_required';
  end if;
  if not v_break_glass and v_break_glass_reason is not null then
    raise exception using errcode = '23514', message = 'unexpected_break_glass_reason';
  end if;

  for v_item in select i.* from public.amendment_items i
   where i.org_id = v_org_id and i.station_id = v_station_id and i.amendment_id = p_amendment_id
   order by i.target_kind, i.target_logical_id, i.field
  loop
    if v_item.target_kind = 'sales_declared' then
      if v_item.field not in ('cash_amount', 'cashless_amount') then
        raise exception using errcode = '23514', message = 'amendment_field_forbidden';
      end if;
      if not exists (select 1 from public.sales_declared x where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.report_id = v_base_report_id and x.sales_id = v_item.target_logical_id) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if not v_break_glass and exists (select 1 from public.sales_declared x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = v_base_report_id and x.sales_id = v_item.target_logical_id and x.created_by = v_requester) then
        raise exception using errcode = '42501', message = 'amendment_creator_forbidden';
      end if;
      if v_item.old_value <> (select to_jsonb(case when v_item.field = 'cash_amount' then x.cash_amount::text else x.cashless_amount::text end) from public.sales_declared x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = v_base_report_id and x.sales_id = v_item.target_logical_id) then
        raise exception using errcode = '23505', message = 'stale_amendment_value';
      end if;
    elsif v_item.target_kind = 'loss_entry' then
      if v_item.field not in ('liters', 'cash_amount', 'note') then
        raise exception using errcode = '23514', message = 'amendment_field_forbidden';
      end if;
      if not exists (select 1 from public.loss_entries x where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.report_id = v_base_report_id and x.row_id = v_item.target_logical_id) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if not v_break_glass and exists (select 1 from public.loss_entries x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = v_base_report_id and x.row_id = v_item.target_logical_id and x.created_by = v_requester) then
        raise exception using errcode = '42501', message = 'amendment_creator_forbidden';
      end if;
      if v_item.old_value <> (select to_jsonb(case when v_item.field = 'liters' then x.liters::text when v_item.field = 'cash_amount' then x.cash_amount::text else x.note end) from public.loss_entries x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = v_base_report_id and x.row_id = v_item.target_logical_id) then
        raise exception using errcode = '23505', message = 'stale_amendment_value';
      end if;
    elsif v_item.target_kind = 'delivery' then
      if v_item.field not in ('reference', 'delivery_id') or not exists (select 1 from public.deliveries x where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.delivery_id = v_item.target_logical_id) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
    elsif v_item.target_kind = 'dip_reading' then
      if v_item.field not in ('reference', 'dip_id') or not exists (select 1 from public.dip_readings x where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.dip_id = v_item.target_logical_id) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
    end if;
  end loop;

  v_new_version := v_base_version + 1;
  perform set_config('app.transition', 'approve_amendment', true);
  insert into public.shift_reports(report_id, org_id, station_id, shift_id, version_no,
    supersedes_report_id, status, submitted_by, policy_snapshot_set_id)
  select v_new_report, r.org_id, r.station_id, r.shift_id, v_new_version,
    r.report_id, 'submitted', v_actor, r.policy_snapshot_set_id
    from public.shift_reports r
   where r.org_id = v_org_id and r.station_id = v_station_id and r.shift_id = v_shift_id and r.report_id = v_base_report_id;
  insert into public.dispenser_readings(
    org_id, station_id, shift_id, report_id, nozzle_id, meter_start, meter_end,
    price_used, expected_sale_rupiah, observed, is_carried_forward,
    source_shift_id, source_report_id, source_reading_id)
  select x.org_id, x.station_id, x.shift_id, v_new_report, x.nozzle_id, x.meter_start,
    x.meter_end, x.price_used, x.expected_sale_rupiah, x.observed, x.is_carried_forward,
    x.source_shift_id, x.source_report_id, x.source_reading_id
    from public.dispenser_readings x
   where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.report_id = v_base_report_id;
  insert into public.sales_declared(org_id, station_id, shift_id, report_id, dispenser_id, cash_amount, cashless_amount, created_by)
  select x.org_id, x.station_id, x.shift_id, v_new_report, x.dispenser_id,
    coalesce((select (i.new_value #>> '{}')::numeric from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'sales_declared' and i.target_logical_id = x.sales_id and i.field = 'cash_amount'), x.cash_amount),
    coalesce((select (i.new_value #>> '{}')::numeric from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'sales_declared' and i.target_logical_id = x.sales_id and i.field = 'cashless_amount'), x.cashless_amount),
    x.created_by
    from public.sales_declared x
   where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.report_id = v_base_report_id;
  insert into public.loss_entries(
    org_id, station_id, shift_id, report_id, version_no, loss_id, nozzle_id,
    direction, reason_code, liters, cash_amount, note, created_by)
  select x.org_id, x.station_id, x.shift_id, v_new_report, v_new_version, x.loss_id,
    x.nozzle_id, x.direction, x.reason_code,
    coalesce((select (i.new_value #>> '{}')::numeric from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'loss_entry' and i.target_logical_id = x.row_id and i.field = 'liters'), x.liters),
    coalesce((select (i.new_value #>> '{}')::numeric from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'loss_entry' and i.target_logical_id = x.row_id and i.field = 'cash_amount'), x.cash_amount),
    case when exists (select 1 from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'loss_entry' and i.target_logical_id = x.row_id and i.field = 'note') then (select i.new_value #>> '{}' from public.amendment_items i where i.amendment_id = p_amendment_id and i.target_kind = 'loss_entry' and i.target_logical_id = x.row_id and i.field = 'note') else x.note end,
    x.created_by
    from public.loss_entries x
   where x.org_id = v_org_id and x.station_id = v_station_id and x.shift_id = v_shift_id and x.report_id = v_base_report_id;
  insert into public.ack_head(org_id, station_id, shift_id, report_id, version_no)
  values (v_org_id, v_station_id, v_shift_id, v_new_report, v_new_version);
  select * into v_old_head from public.ack_head h
   where h.org_id = v_org_id and h.station_id = v_station_id and h.shift_id = v_shift_id
     and h.report_id = v_base_report_id and h.version_no = v_base_version for update;
  if v_old_head.active_ack_id is not null then
    insert into public.ack_supersessions(
      old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no,
      superseded_ack_id, replacement_org_id, replacement_station_id, replacement_shift_id,
      replacement_report_id, replacement_version_no, reason)
    values (v_org_id, v_station_id, v_shift_id, v_base_report_id, v_base_version,
      v_old_head.active_ack_id, v_org_id, v_station_id, v_shift_id, v_new_report,
      v_new_version, 'amendment');
    update public.ack_head h set active_ack_id = null
     where h.org_id = v_org_id and h.station_id = v_station_id and h.shift_id = v_shift_id
       and h.report_id = v_base_report_id and h.version_no = v_base_version;
  end if;
  update public.shifts set current_report_id = v_new_report
   where org_id = v_org_id and station_id = v_station_id and shift_id = v_shift_id;
  perform public.fn_transition_shift(v_shift_id, 'awaiting_confirmation'::public.shift_status, 'amendment');
  perform set_config('app.transition', 'approve_amendment', true);
  update public.amendments a set status = 'approved', approver_user_id = v_actor,
    decided_at = clock_timestamp(), applied_report_id = v_new_report
   where a.org_id = v_org_id and a.station_id = v_station_id and a.amendment_id = p_amendment_id;
  amendment_id := p_amendment_id;
  applied_report_id := v_new_report;
  version_no := v_new_version;
  return next;
end;
$$;
alter function public.fn_approve_amendment(uuid, bytea) owner to report_writer;

do $$
declare v_table text;
begin
  foreach v_table in array array['amendments', 'amendment_items'] loop
    execute format('alter table public.%I enable row level security', v_table);
    execute format('alter table public.%I force row level security', v_table);
  end loop;
end
$$;
create policy amendments_context on public.amendments
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy amendment_items_context on public.amendment_items
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));

revoke all on table public.amendments, public.amendment_items from public, pomkita_app, report_writer, audit_owner, relay;
grant select, update on public.amendments to report_writer;
grant select on public.amendment_items to report_writer;
grant execute on function public.fn_transition_shift(uuid, public.shift_status, text) to report_writer;
grant execute on function public.fn_approve_amendment(uuid, bytea) to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_approve_amendment', array['Station Admin', 'Owner', 'Superadmin'], 'approve_amendment', 49),
  ('fn_amendment_base_payload', array[]::text[], 'internal_payload', 50),
  ('trg_amendment_guard', array[]::text[], 'internal_trigger', 49)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
