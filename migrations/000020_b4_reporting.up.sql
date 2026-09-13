-- PLAN.md sections 7, 9, 10, and 11; BE-PLAN.md Phase B4.

create or replace function public.read_report(p_report_id uuid)
returns jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid := nullif(current_setting('app.station_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_report jsonb;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Supervisor', 'Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'report_role_required';
  end if;
  select jsonb_build_object(
    'report_id', r.report_id::text,
    'shift_id', r.shift_id::text,
    'version_no', r.version_no,
    'status', r.status,
    'submitted_at', r.submitted_at,
    'readings', coalesce((
      select jsonb_agg(jsonb_build_object(
        'nozzle_id', x.nozzle_id::text,
        'meter_start', x.meter_start::text,
        'meter_end', x.meter_end::text,
        'price_used', x.price_used::text,
        'expected_sale_rupiah', x.expected_sale_rupiah::text,
        'observed', x.observed,
        'is_carried_forward', x.is_carried_forward,
        'source_shift_id', x.source_shift_id::text,
        'source_report_id', x.source_report_id::text,
        'source_reading_id', x.source_reading_id::text)
        order by x.nozzle_id)
      from public.dispenser_readings x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.report_id = r.report_id
    ), '[]'::jsonb),
    'sales', coalesce((
      select jsonb_agg(jsonb_build_object(
        'sales_id', x.sales_id::text,
        'dispenser_id', x.dispenser_id::text,
        'cash_amount', x.cash_amount::text,
        'cashless_amount', x.cashless_amount::text)
        order by x.dispenser_id)
      from public.sales_declared x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.report_id = r.report_id
    ), '[]'::jsonb),
    'losses', coalesce((
      select jsonb_agg(jsonb_build_object(
        'row_id', x.row_id::text,
        'loss_id', x.loss_id::text,
        'nozzle_id', x.nozzle_id::text,
        'direction', x.direction,
        'reason_code', x.reason_code,
        'liters', x.liters::text,
        'cash_amount', x.cash_amount::text,
        'note', x.note)
        order by x.row_id)
      from public.loss_entries x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.report_id = r.report_id
    ), '[]'::jsonb),
    'deliveries', coalesce((
      select jsonb_agg(jsonb_build_object(
        'delivery_id', x.delivery_id::text,
        'do_number', x.do_number,
        'tank_id', x.tank_id::text,
        'liters', x.liters::text)
        order by x.delivery_id)
      from public.deliveries x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id
    ), '[]'::jsonb),
    'dip_readings', coalesce((
      select jsonb_agg(jsonb_build_object(
        'dip_id', x.dip_id::text,
        'tank_id', x.tank_id::text,
        'dip_liters', x.dip_liters::text)
        order by x.dip_id)
      from public.dip_readings x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id
    ), '[]'::jsonb),
    'evidence_events', coalesce((
      select jsonb_agg(jsonb_build_object(
        'evidence_event_id', x.evidence_event_id::text,
        'evidence_id', x.evidence_id::text,
        'loss_row_id', x.loss_row_id::text,
        'event_seq', x.event_seq,
        'event_type', x.event_type,
        'evidence_type', x.evidence_type,
        'object_key', x.object_key,
        'content_hash', encode(x.content_hash, 'hex'),
        'size_bytes', x.size_bytes::text,
        'mime', x.mime,
        'at', x.at)
        order by x.loss_row_id, x.evidence_id, x.event_seq)
      from public.evidence_event x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.report_id = r.report_id
    ), '[]'::jsonb),
    'loss_exceptions', coalesce((
      select jsonb_agg(jsonb_build_object(
        'exception_id', x.exception_id::text,
        'loss_row_id', x.loss_row_id::text,
        'reason', x.reason,
        'actor_user_id', x.actor_user_id::text,
        'created_at', x.created_at)
        order by x.loss_row_id)
      from public.loss_exception x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.report_id = r.report_id
    ), '[]'::jsonb),
    'policy_snapshot', coalesce((
      select jsonb_object_agg(x.policy_kind, jsonb_build_object(
        'policy_id', x.policy_id::text,
        'revision_id', x.rev_id::text,
        'scope', x.scope,
        'payload', x.payload,
        'payload_hash', encode(x.payload_hash, 'hex')))
      from public.policy_snapshot_items x
      where x.org_id = r.org_id and x.station_id = r.station_id
        and x.shift_id = r.shift_id and x.set_id = r.policy_snapshot_set_id
    ), '{}'::jsonb),
    'ack_state', coalesce((
      select jsonb_build_object(
        'active_ack_id', h.active_ack_id::text,
        'decision', d.decision::text,
        'actor_user_id', d.actor_user_id::text,
        'decided_at', d.decided_at,
        'rejection_reason', d.rejection_reason,
        'is_break_glass', d.is_break_glass,
        'break_glass_reason', d.break_glass_reason)
      from public.ack_head h
      left join public.ack_decisions d
        on d.org_id = h.org_id and d.station_id = h.station_id
       and d.shift_id = h.shift_id and d.report_id = h.report_id
       and d.version_no = h.version_no and d.ack_id = h.active_ack_id
      where h.org_id = r.org_id and h.station_id = r.station_id
        and h.shift_id = r.shift_id and h.report_id = r.report_id
        and h.version_no = r.version_no
    ), '{}'::jsonb)
  )
    into v_report
    from public.shift_reports r
   where r.org_id = v_org_id and r.report_id = p_report_id
     and (v_station_id is null or r.station_id = v_station_id);
  if v_report is null then
    raise exception using errcode = '42501', message = 'report_not_found';
  end if;
  return v_report;
end;
$$;
alter function public.read_report(uuid) owner to report_writer;

create or replace function public.read_report_printout(p_report_id uuid)
returns jsonb
language sql
security definer
set search_path = pg_catalog, public, app
as $function$
  select public.read_report(p_report_id)
$function$;
alter function public.read_report_printout(uuid) owner to report_writer;

create or replace function public.read_anomaly_export()
returns table (
  kind text, source text, source_id uuid, org_id uuid, station_id uuid,
  shift_id uuid, report_id uuid, version_no integer, reason text,
  variance_rupiah numeric, threshold numeric, happened_at timestamptz(6)
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid := nullif(current_setting('app.station_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if current_setting('app.role', true) not in ('Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'anomaly_export_role_required';
  end if;
  return query
    select 'break_glass', 'ack_decision', d.ack_id, d.org_id, d.station_id,
           d.shift_id, d.report_id, d.version_no, d.break_glass_reason,
           null::numeric, null::numeric, d.decided_at
      from public.ack_decisions d
     where d.org_id = v_org_id and d.is_break_glass
       and (v_station_id is null or d.station_id = v_station_id)
    union all
    select 'break_glass', 'amendment', a.amendment_id, a.org_id, a.station_id,
           a.shift_id, a.base_report_id, r.version_no, a.break_glass_reason,
           null::numeric, null::numeric, coalesce(a.decided_at, a.requested_at)
      from public.amendments a
      join public.shift_reports r
        on r.org_id = a.org_id and r.station_id = a.station_id
       and r.shift_id = a.shift_id and r.report_id = a.base_report_id
     where a.org_id = v_org_id and a.is_break_glass
       and (v_station_id is null or a.station_id = v_station_id)
    union all
    select 'loss_exception', 'loss_exception', x.exception_id, x.org_id,
           x.station_id, x.shift_id, x.report_id, r.version_no, x.reason,
           null::numeric, null::numeric, x.created_at
      from public.loss_exception x
      join public.shift_reports r
        on r.org_id = x.org_id and r.station_id = x.station_id
       and r.shift_id = x.shift_id and r.report_id = x.report_id
     where x.org_id = v_org_id
       and (v_station_id is null or x.station_id = v_station_id)
    union all
    select 'variance', 'report', r.report_id, r.org_id, r.station_id,
           r.shift_id, r.report_id, r.version_no, null::text,
           v.variance, v.threshold, r.submitted_at
      from public.shift_reports r
      cross join lateral (
        select coalesce((select sum(d.expected_sale_rupiah)
                           from public.dispenser_readings d
                          where d.org_id = r.org_id and d.station_id = r.station_id
                            and d.shift_id = r.shift_id and d.report_id = r.report_id), 0)
             - coalesce((select sum(s.cash_amount + s.cashless_amount)
                           from public.sales_declared s
                          where s.org_id = r.org_id and s.station_id = r.station_id
                            and s.shift_id = r.shift_id and s.report_id = r.report_id), 0) as variance,
          coalesce((select (i.payload->>'variance_rupiah_threshold')::numeric
                      from public.policy_snapshot_items i
                     where i.org_id = r.org_id and i.station_id = r.station_id
                       and i.shift_id = r.shift_id and i.set_id = r.policy_snapshot_set_id
                       and i.policy_kind = 'threshold'), 0) as threshold
      ) v
     where r.org_id = v_org_id and abs(v.variance) > v.threshold
       and (v_station_id is null or r.station_id = v_station_id)
     order by happened_at, source_id;
end;
$$;
alter function public.read_anomaly_export() owner to report_writer;

create or replace function public.read_audit_export()
returns table (
  event_id uuid, org_sequence bigint, event_type text, payload jsonb,
  outcome text, outcome_error text, created_at timestamptz(6),
  prev_hash text, row_hash text
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if current_setting('app.context_valid', true) <> 'true'
     or nullif(current_setting('app.org_id', true), '') is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if current_setting('app.role', true) not in ('Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'audit_export_role_required';
  end if;
  return query
    select a.event_id, a.org_sequence, a.event_type, a.payload,
           a.outcome, a.outcome_error, a.created_at,
           encode(a.prev_hash, 'hex'), encode(a.row_hash, 'hex')
      from public.audit_log a
     where a.org_id = nullif(current_setting('app.org_id', true), '')::uuid
     order by a.org_sequence;
end;
$$;
alter function public.read_audit_export() owner to audit_owner;

create or replace function public.read_policy_history()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid := nullif(current_setting('app.station_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if current_setting('app.role', true) not in ('Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'policy_history_role_required';
  end if;
  return query
    select jsonb_build_object(
      'policy_kind', 'threshold', 'policy_id', p.policy_id::text,
      'revision_id', p.rev_id::text, 'station_id', p.station_id::text,
      'valid_from', p.valid_from, 'supersedes_revision_id', p.supersedes_rev_id::text,
      'disabled', p.disabled, 'loss_liter_threshold', p.loss_liter_threshold::text,
      'gain_liter_threshold', p.gain_liter_threshold::text,
      'loss_rupiah_threshold', p.loss_rupiah_threshold::text,
      'gain_rupiah_threshold', p.gain_rupiah_threshold::text,
      'variance_rupiah_threshold', p.variance_rupiah_threshold::text,
      'rollover_threshold', p.rollover_threshold::text,
      'created_by', p.created_by::text, 'created_at', p.created_at)
      from public.threshold_policy_revisions p
     where p.org_id = v_org_id and (v_station_id is null or p.station_id is null or p.station_id = v_station_id)
    union all
    select jsonb_build_object(
      'policy_kind', 'evidence', 'policy_id', p.policy_id::text,
      'revision_id', p.rev_id::text, 'station_id', p.station_id::text,
      'valid_from', p.valid_from, 'supersedes_revision_id', p.supersedes_rev_id::text,
      'disabled', p.disabled, 'mode', p.mode,
      'types', coalesce((select jsonb_agg(jsonb_build_object(
        'evidence_type', x.evidence_type,
        'minimum_count_per_loss', x.minimum_count_per_loss,
        'accepted_mime_types', x.accepted_mime_types)
        order by x.evidence_type)
        from public.evidence_policy_types x
       where x.org_id = p.org_id and x.rev_id = p.rev_id), '[]'::jsonb),
      'created_by', p.created_by::text, 'created_at', p.created_at)
      from public.evidence_policy_revisions p
     where p.org_id = v_org_id and (v_station_id is null or p.station_id is null or p.station_id = v_station_id)
     order by 1->>'valid_from', 1->>'policy_kind';
end;
$$;
alter function public.read_policy_history() owner to report_writer;

create or replace function public.read_audit_chain()
returns table (event_id uuid, org_sequence bigint, event_type text, payload jsonb,
  outcome text, outcome_error text, created_at timestamptz(6), prev_hash bytea, row_hash bytea)
language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
begin
  if current_setting('app.context_valid', true) <> 'true'
     or nullif(current_setting('app.org_id', true), '') is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if current_setting('app.role', true) not in ('Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'audit_chain_role_required';
  end if;
  return query
    select a.event_id, a.org_sequence, a.event_type, a.payload, a.outcome,
           a.outcome_error, a.created_at, a.prev_hash, a.row_hash
      from public.audit_log a
     where a.org_id = nullif(current_setting('app.org_id', true), '')::uuid
     order by a.org_sequence;
end;
$$;
alter function public.read_audit_chain() owner to audit_owner;

revoke all on function public.read_report(uuid), public.read_report_printout(uuid),
  public.read_anomaly_export(), public.read_audit_export(), public.read_policy_history(),
  public.read_audit_chain() from public, pomkita_app, report_writer, audit_owner, relay;
grant execute on function public.read_report(uuid), public.read_report_printout(uuid),
  public.read_anomaly_export(), public.read_audit_export(), public.read_policy_history(),
  public.read_audit_chain() to pomkita_app;
grant execute on function public.read_report(uuid) to report_writer;

grant select on public.shift_reports, public.dispenser_readings, public.sales_declared,
  public.loss_entries, public.deliveries, public.dip_readings, public.evidence_event,
  public.loss_exception, public.policy_snapshot_items, public.ack_head, public.ack_decisions
  to report_writer;
grant select on public.audit_log to audit_owner;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('read_report_printout', array['Supervisor', 'Station Admin', 'Owner', 'Superadmin'], 'read_report_printout', 50),
  ('read_anomaly_export', array['Station Admin', 'Owner', 'Superadmin'], 'read_anomaly_export', 53),
  ('read_audit_export', array['Station Admin', 'Owner', 'Superadmin'], 'read_audit_export', 100),
  ('read_policy_history', array['Owner', 'Superadmin'], 'read_policy_history', 47)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;

update public.procedure_registry
   set allowed_roles = array['Station Admin', 'Owner', 'Superadmin'],
       action = 'read_audit_chain'
 where name = 'read_audit_chain';
