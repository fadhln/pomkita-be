-- PLAN.md sections 7, 9, 10, and 11; BE-PLAN.md Phase B4.

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
    select export_row.kind, export_row.source, export_row.source_id,
           export_row.org_id, export_row.station_id, export_row.shift_id,
           export_row.report_id, export_row.version_no, export_row.reason,
           export_row.variance_rupiah, export_row.threshold, export_row.happened_at
      from (
        select 'break_glass'::text as kind, 'ack_decision'::text as source,
               d.ack_id as source_id, d.org_id, d.station_id, d.shift_id,
               d.report_id, d.version_no, d.break_glass_reason as reason,
               null::numeric as variance_rupiah, null::numeric as threshold,
               d.decided_at as happened_at
          from public.ack_decisions d
         where d.org_id = v_org_id and d.is_break_glass
           and (v_station_id is null or d.station_id = v_station_id)
        union all
        select 'break_glass', 'amendment', a.amendment_id, a.org_id,
               a.station_id, a.shift_id, a.base_report_id, r.version_no,
               a.break_glass_reason, null::numeric, null::numeric,
               coalesce(a.decided_at, a.requested_at)
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
               variance.variance_rupiah, variance.threshold, r.submitted_at
          from public.shift_reports r
          cross join lateral (
            select coalesce((select sum(d.expected_sale_rupiah)
                               from public.dispenser_readings d
                              where d.org_id = r.org_id
                                and d.station_id = r.station_id
                                and d.shift_id = r.shift_id
                                and d.report_id = r.report_id), 0)
                 - coalesce((select sum(s.cash_amount + s.cashless_amount)
                               from public.sales_declared s
                              where s.org_id = r.org_id
                                and s.station_id = r.station_id
                                and s.shift_id = r.shift_id
                                and s.report_id = r.report_id), 0)
                 as variance_rupiah,
              coalesce((select (i.payload->>'variance_rupiah_threshold')::numeric
                          from public.policy_snapshot_items i
                         where i.org_id = r.org_id
                           and i.station_id = r.station_id
                           and i.shift_id = r.shift_id
                           and i.set_id = r.policy_snapshot_set_id
                           and i.policy_kind = 'threshold'), 0) as threshold
          ) variance
         where r.org_id = v_org_id and abs(variance.variance_rupiah) > variance.threshold
           and (v_station_id is null or r.station_id = v_station_id)
      ) export_row
     order by export_row.happened_at, export_row.source_id;
end;
$$;

alter function public.read_anomaly_export() owner to report_writer;

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
    select item.payload
      from (
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
          'created_by', p.created_by::text, 'created_at', p.created_at) as payload,
          p.valid_from as sort_at, 1 as sort_kind
          from public.threshold_policy_revisions p
         where p.org_id = v_org_id
           and (v_station_id is null or p.station_id is null or p.station_id = v_station_id)
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
          'created_by', p.created_by::text, 'created_at', p.created_at) as payload,
          p.valid_from as sort_at, 2 as sort_kind
          from public.evidence_policy_revisions p
         where p.org_id = v_org_id
           and (v_station_id is null or p.station_id is null or p.station_id = v_station_id)
      ) item
     order by item.sort_at, item.sort_kind;
end;
$$;

alter function public.read_policy_history() owner to report_writer;
