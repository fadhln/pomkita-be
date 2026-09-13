-- PLAN.md section 3.3; BE-PLAN.md Phase B2.

create or replace function public.trg_amendment_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op in ('UPDATE', 'DELETE')
     and current_setting('app.transition', true) not in
         ('approve_amendment', 'request_amendment', 'reject_amendment') then
    raise exception using errcode = '23514', message = 'amendment_write_forbidden';
  end if;
  return coalesce(new, old);
end;
$$;
alter function public.trg_amendment_guard() owner to report_writer;

create or replace function public.fn_request_amendment(
  p_shift_id uuid,
  p_base_report_id uuid,
  p_reason text,
  p_items jsonb
)
returns table (
  amendment_id uuid,
  base_report_id uuid,
  status text,
  stale_check_hash bytea,
  requested_at timestamptz(6)
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_claim_station uuid := nullif(current_setting('app.station_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_station_id uuid;
  v_current_report uuid;
  v_shift_status text;
  v_base_version integer;
  v_base_status text;
  v_payload jsonb;
  v_hash bytea;
  v_item jsonb;
  v_target_id uuid;
  v_pending_id uuid;
  v_amendment_id uuid := app.gen_random_uuid();
  v_requested_at timestamptz(6);
  v_item_count integer;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if p_shift_id is null or p_base_report_id is null
     or btrim(coalesce(p_reason, '')) = ''
     or p_items is null
     or jsonb_typeof(p_items) <> 'array'
     or jsonb_array_length(p_items) = 0 then
    raise exception using errcode = '22023', message = 'invalid_amendment_request';
  end if;
  if v_role <> 'Supervisor' then
    raise exception using errcode = '42501', message = 'amendment_request_role_required';
  end if;

  select s.station_id
    into v_station_id
    from public.shifts s
   where s.org_id = v_org_id and s.shift_id = p_shift_id;
  if not found or (v_claim_station is not null and v_claim_station <> v_station_id) then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations s
   where s.org_id = v_org_id and s.station_id = v_station_id for update;
  select s.current_report_id, s.status::text
    into v_current_report, v_shift_status
    from public.shifts s
   where s.org_id = v_org_id and s.station_id = v_station_id and s.shift_id = p_shift_id
   for update;
  if v_current_report <> p_base_report_id
     or v_shift_status not in ('awaiting_confirmation', 'needs_correction', 'locked') then
    raise exception using errcode = '23505', message = 'amendment_base_not_current';
  end if;
  if not exists (
    select 1 from public.shifts s
     where s.org_id = v_org_id and s.station_id = v_station_id and s.shift_id = p_shift_id
       and s.supervisor_id = v_actor
  ) then
    raise exception using errcode = '42501', message = 'amendment_request_role_required';
  end if;
  select r.version_no, r.status
    into v_base_version, v_base_status
    from public.shift_reports r
   where r.org_id = v_org_id and r.station_id = v_station_id
     and r.shift_id = p_shift_id and r.report_id = p_base_report_id
   for update;
  if not found or v_base_status not in ('submitted', 'locked') then
    raise exception using errcode = '23505', message = 'amendment_base_not_current';
  end if;

  for v_item in select value from jsonb_array_elements(p_items) loop
    if jsonb_typeof(v_item) <> 'object'
       or btrim(coalesce(v_item->>'target_kind', '')) = ''
       or btrim(coalesce(v_item->>'target_logical_id', '')) = ''
       or btrim(coalesce(v_item->>'field', '')) = ''
       or not (v_item ? 'old_value')
       or not (v_item ? 'new_value') then
      raise exception using errcode = '22023', message = 'invalid_amendment_request';
    end if;
    if (v_item->>'target_logical_id') !~* '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' then
      raise exception using errcode = '22023', message = 'invalid_amendment_request';
    end if;
    v_target_id := (v_item->>'target_logical_id')::uuid;
    if v_item->>'target_kind' = 'sales_declared' then
      if v_item->>'field' not in ('cash_amount', 'cashless_amount') then
        raise exception using errcode = '23514', message = 'amendment_field_forbidden';
      end if;
      if not exists (
        select 1 from public.sales_declared x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.report_id = p_base_report_id
           and x.sales_id = v_target_id
      ) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if v_item->'old_value' <> (
        select to_jsonb(case when v_item->>'field' = 'cash_amount'
                            then x.cash_amount::text else x.cashless_amount::text end)
          from public.sales_declared x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.report_id = p_base_report_id
           and x.sales_id = v_target_id
      ) then
        raise exception using errcode = '23505', message = 'stale_amendment_value';
      end if;
    elsif v_item->>'target_kind' = 'loss_entry' then
      if v_item->>'field' not in ('liters', 'cash_amount', 'note') then
        raise exception using errcode = '23514', message = 'amendment_field_forbidden';
      end if;
      if not exists (
        select 1 from public.loss_entries x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.report_id = p_base_report_id
           and x.row_id = v_target_id
      ) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if v_item->'old_value' <> (
        select case when v_item->>'field' = 'liters' then to_jsonb(x.liters::text)
                    when v_item->>'field' = 'cash_amount' then to_jsonb(x.cash_amount::text)
                    else to_jsonb(x.note) end
          from public.loss_entries x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.report_id = p_base_report_id
           and x.row_id = v_target_id
      ) then
        raise exception using errcode = '23505', message = 'stale_amendment_value';
      end if;
    elsif v_item->>'target_kind' in ('delivery', 'dip_reading') then
      if v_item->>'field' not in ('reference', 'delivery_id', 'dip_id') then
        raise exception using errcode = '23514', message = 'amendment_field_forbidden';
      end if;
      if v_item->>'target_kind' = 'delivery' and not exists (
        select 1 from public.deliveries x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.delivery_id = v_target_id
      ) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if v_item->>'target_kind' = 'dip_reading' and not exists (
        select 1 from public.dip_readings x
         where x.org_id = v_org_id and x.station_id = v_station_id
           and x.shift_id = p_shift_id and x.dip_id = v_target_id
      ) then
        raise exception using errcode = '23514', message = 'amendment_target_not_in_base';
      end if;
      if v_item->'old_value' <> to_jsonb(v_target_id::text) then
        raise exception using errcode = '23505', message = 'stale_amendment_value';
      end if;
    else
      raise exception using errcode = '23514', message = 'amendment_field_forbidden';
    end if;
  end loop;

  v_payload := public.fn_amendment_base_payload(v_org_id, v_station_id, p_shift_id, p_base_report_id);
  v_hash := app.digest(convert_to(jsonb_build_array(v_payload, v_base_version)::text, 'UTF8'), 'sha256');
  v_item_count := jsonb_array_length(p_items);
  perform set_config('app.transition', 'request_amendment', true);
  select a.amendment_id into v_pending_id
    from public.amendments a
   where a.org_id = v_org_id and a.station_id = v_station_id
     and a.base_report_id = p_base_report_id and a.status = 'pending'
   for update;
  if v_pending_id is not null then
    update public.amendments as a
       set status = 'superseded', decided_at = clock_timestamp()
     where a.org_id = v_org_id and a.station_id = v_station_id
       and a.amendment_id = v_pending_id;
  end if;
  insert into public.amendments as a (
    amendment_id, org_id, station_id, shift_id, base_report_id, reason,
    requester_user_id, stale_check_hash)
  values (
    v_amendment_id, v_org_id, v_station_id, p_shift_id, p_base_report_id,
    p_reason, v_actor, v_hash)
  returning a.requested_at into v_requested_at;
  insert into public.amendment_items(
    amendment_id, org_id, station_id, shift_id, target_kind, target_logical_id,
    field, old_value, new_value)
  select v_amendment_id, v_org_id, v_station_id, p_shift_id,
         x->>'target_kind', (x->>'target_logical_id')::uuid, x->>'field',
         x->'old_value', x->'new_value'
    from jsonb_array_elements(p_items) x;
  perform public.fn_append_audit_event(
    v_amendment_id, 'amendment_requested',
    jsonb_build_object('shift_id', p_shift_id::text, 'base_report_id', p_base_report_id::text,
                       'item_count', v_item_count), 'success', null);
  amendment_id := v_amendment_id;
  base_report_id := p_base_report_id;
  status := 'pending';
  stale_check_hash := v_hash;
  requested_at := v_requested_at;
  return next;
end;
$$;
alter function public.fn_request_amendment(uuid, uuid, text, jsonb) owner to report_writer;

create or replace function public.fn_reject_amendment(
  p_amendment_id uuid,
  p_rejection_reason text
)
returns table (
  amendment_id uuid,
  status text,
  rejection_reason text,
  decided_at timestamptz(6)
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_claim_station uuid := nullif(current_setting('app.station_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_station_id uuid;
  v_requester uuid;
  v_status text;
  v_decided_at timestamptz(6);
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Station Admin', 'Owner') then
    raise exception using errcode = '42501', message = 'amendment_reject_role_required';
  end if;
  if p_amendment_id is null or btrim(coalesce(p_rejection_reason, '')) = '' then
    raise exception using errcode = '23514', message = 'rejection_reason_required';
  end if;
  select a.station_id, a.requester_user_id, a.status
    into v_station_id, v_requester, v_status
    from public.amendments a
   where a.org_id = v_org_id and a.amendment_id = p_amendment_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'amendment_not_found';
  end if;
  if v_claim_station is not null and v_claim_station <> v_station_id then
    raise exception using errcode = '42501', message = 'amendment_not_found';
  end if;
  if v_status <> 'pending' then
    raise exception using errcode = '23505', message = 'amendment_not_pending';
  end if;
  if v_actor = v_requester then
    raise exception using errcode = '42501', message = 'amendment_separation_required';
  end if;
  if v_role = 'Station Admin' and not exists (
    select 1 from public.user_station_roles r
     where r.org_id = v_org_id and r.station_id = v_station_id
       and r.user_id = v_actor and r.role = 'Station Admin'
  ) then
    raise exception using errcode = '42501', message = 'amendment_reject_role_required';
  end if;
  if v_role = 'Owner' and not exists (
    select 1 from public.user_station_roles r
     where r.org_id = v_org_id and r.user_id = v_actor and r.role = 'Owner'
  ) then
    raise exception using errcode = '42501', message = 'amendment_reject_role_required';
  end if;
  perform set_config('app.transition', 'reject_amendment', true);
  update public.amendments
     set status = 'rejected', approver_user_id = v_actor,
         decided_at = clock_timestamp(), rejection_reason = p_rejection_reason
   where org_id = v_org_id and station_id = v_station_id and amendment_id = p_amendment_id;
  select a.status, a.rejection_reason, a.decided_at
    into v_status, p_rejection_reason, v_decided_at
    from public.amendments a
   where a.org_id = v_org_id and a.station_id = v_station_id and a.amendment_id = p_amendment_id;
  perform public.fn_append_audit_event(
    p_amendment_id, 'amendment_rejected',
    jsonb_build_object('shift_id', (select shift_id::text from public.amendments where amendment_id = p_amendment_id),
                       'rejection_reason', p_rejection_reason), 'success', null);
  amendment_id := p_amendment_id;
  status := v_status;
  rejection_reason := p_rejection_reason;
  decided_at := v_decided_at;
  return next;
end;
$$;
alter function public.fn_reject_amendment(uuid, text) owner to report_writer;

create or replace function public.read_amendment_queue()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_claim_station uuid := nullif(current_setting('app.station_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Station Admin', 'Owner') then
    raise exception using errcode = '42501', message = 'amendment_queue_role_required';
  end if;
  return query
    select jsonb_build_object(
      'amendment_id', a.amendment_id::text,
      'station_id', a.station_id::text,
      'shift_id', a.shift_id::text,
      'base_report_id', a.base_report_id::text,
      'base_version_no', r.version_no,
      'status', a.status,
      'requester', jsonb_build_object('user_id', u.user_id::text, 'display_name', u.display_name),
      'reason', a.reason,
      'requested_at', a.requested_at,
      'items', coalesce((
        select jsonb_agg(jsonb_build_object(
          'item_id', i.item_id::text, 'target_kind', i.target_kind,
          'target_logical_id', i.target_logical_id::text, 'field', i.field,
          'old_value', i.old_value, 'new_value', i.new_value)
          order by i.target_kind, i.target_logical_id, i.field)
          from public.amendment_items i
         where i.org_id = a.org_id and i.station_id = a.station_id
           and i.amendment_id = a.amendment_id
      ), '[]'::jsonb)
    )
      from public.amendments a
      join public.shift_reports r
        on r.org_id = a.org_id and r.station_id = a.station_id
       and r.shift_id = a.shift_id and r.report_id = a.base_report_id
      join public.users u on u.org_id = a.org_id and u.user_id = a.requester_user_id
     where a.org_id = v_org_id and a.status = 'pending'
       and (v_claim_station is null or a.station_id = v_claim_station)
       and ((v_role = 'Owner' and exists (
              select 1 from public.user_station_roles x
               where x.org_id = v_org_id and x.user_id = v_actor and x.role = 'Owner'))
         or (v_role = 'Station Admin' and exists (
              select 1 from public.user_station_roles x
               where x.org_id = v_org_id and x.station_id = a.station_id
                 and x.user_id = v_actor and x.role = 'Station Admin')))
     order by a.requested_at, a.amendment_id;
end;
$$;
alter function public.read_amendment_queue() owner to report_writer;

grant select on public.users, public.user_station_roles to report_writer;
grant insert, select, update on public.amendments to report_writer;
grant insert, select on public.amendment_items to report_writer;
grant execute on function public.fn_request_amendment(uuid, uuid, text, jsonb) to pomkita_app;
grant execute on function public.fn_reject_amendment(uuid, text) to pomkita_app;
grant execute on function public.read_amendment_queue() to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_request_amendment', array['Supervisor'], 'request_amendment', 49),
  ('read_amendment_queue', array['Station Admin', 'Owner'], 'read_amendment_queue', 52),
  ('fn_reject_amendment', array['Station Admin', 'Owner'], 'reject_amendment', 49)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
