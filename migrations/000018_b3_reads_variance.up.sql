-- PLAN.md sections 3.3, 4.2, 5.3, 6, 7, and 8; BE-PLAN.md Phase B3.

create or replace function public.fn_evaluate_variance_alert(p_report_id uuid)
returns uuid
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_shift_id uuid;
  v_version integer;
  v_submitted_at timestamptz(6);
  v_rule public.alert_rules%rowtype;
  v_threshold numeric;
  v_variance numeric;
  v_result record;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select r.station_id, r.shift_id, r.version_no, r.submitted_at into v_station_id, v_shift_id, v_version, v_submitted_at
    from public.shift_reports r where r.org_id = v_org_id and r.report_id = p_report_id;
  if not found then
    raise exception using errcode = '42501', message = 'report_not_found';
  end if;
  select * into v_rule from public.alert_rules r
   where r.org_id = v_org_id and r.station_id = v_station_id
     and r.rule_type = 'variance' and r.enabled order by r.created_at, r.rule_id limit 1;
  if not found then
    return null;
  end if;
  v_threshold := v_rule.threshold;
  select coalesce((select sum(d.expected_sale_rupiah) from public.dispenser_readings d where d.org_id=v_org_id and d.station_id=v_station_id and d.report_id=p_report_id),0)
       - coalesce((select sum(s.cash_amount+s.cashless_amount) from public.sales_declared s where s.org_id=v_org_id and s.station_id=v_station_id and s.report_id=p_report_id),0)
    into v_variance;
  if abs(v_variance) <= v_threshold then
    return null;
  end if;
  select * into v_result from public.fn_record_alert_occurrence(
    v_rule.rule_id, 'report', p_report_id, 'fired', v_submitted_at,
    'report', p_report_id, v_version, v_submitted_at,
    nullif(current_setting('app.user_id', true), '')::uuid, null);
  return v_result.event_id;
end;
$$;
alter function public.fn_evaluate_variance_alert(uuid) owner to report_writer;

create or replace function public.trg_report_variance_alert()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  perform public.fn_evaluate_variance_alert(new.report_id);
  return new;
end;
$$;
alter function public.trg_report_variance_alert() owner to report_writer;
create constraint trigger shift_report_variance_alert after insert on public.shift_reports
deferrable initially deferred for each row execute function public.trg_report_variance_alert();

create or replace function public.trg_amendment_evidence_clone()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_base_report uuid;
begin
  if current_setting('app.transition', true) <> 'approve_amendment' then
    return new;
  end if;
  select r.supersedes_report_id into v_base_report
    from public.shift_reports r
   where r.org_id = new.org_id and r.station_id = new.station_id
     and r.shift_id = new.shift_id and r.report_id = new.report_id;
  if v_base_report is null then
    return new;
  end if;
  insert into public.evidence_event(
    evidence_event_id,evidence_id,org_id,station_id,shift_id,report_id,loss_row_id,
    event_seq,event_type,evidence_type,object_key,content_hash,size_bytes,mime,actor_user_id,at)
  select app.gen_random_uuid(), e.evidence_id, e.org_id, e.station_id, e.shift_id,
    new.report_id, new.row_id, e.event_seq, e.event_type, e.evidence_type,
    e.object_key, e.content_hash, e.size_bytes, e.mime, e.actor_user_id, e.at
    from public.evidence_event e
   where e.org_id = new.org_id and e.station_id = new.station_id
     and e.shift_id = new.shift_id and e.report_id = v_base_report
     and e.loss_row_id = (select old_loss.row_id from public.loss_entries old_loss
                           where old_loss.org_id=new.org_id and old_loss.station_id=new.station_id
                             and old_loss.shift_id=new.shift_id and old_loss.report_id=v_base_report
                             and old_loss.loss_id=new.loss_id)
   order by e.evidence_id, e.event_seq;
  insert into public.loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id,created_at)
  select app.gen_random_uuid(), x.org_id, x.station_id, x.shift_id, new.report_id,
    new.row_id, x.reason, x.actor_user_id, x.created_at
    from public.loss_exception x
   where x.org_id = new.org_id and x.station_id = new.station_id
     and x.shift_id = new.shift_id and x.report_id = v_base_report
     and x.loss_row_id = (select old_loss.row_id from public.loss_entries old_loss
                           where old_loss.org_id=new.org_id and old_loss.station_id=new.station_id
                             and old_loss.shift_id=new.shift_id and old_loss.report_id=v_base_report
                             and old_loss.loss_id=new.loss_id)
  on conflict do nothing;
  return new;
end;
$$;
alter function public.trg_amendment_evidence_clone() owner to report_writer;
create trigger loss_entries_amendment_evidence after insert on public.loss_entries
for each row execute function public.trg_amendment_evidence_clone();

create or replace function public.read_anomalies()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  return query
    select jsonb_build_object('kind','break_glass','source','ack_decision','source_id',d.ack_id::text,'shift_id',d.shift_id::text,'report_id',d.report_id::text,'version_no',d.version_no,'reason',d.break_glass_reason,'at',d.decided_at)
      from public.ack_decisions d
     where d.org_id=v_org_id and d.is_break_glass
    union all
    select jsonb_build_object('kind','break_glass','source','amendment','source_id',a.amendment_id::text,'shift_id',a.shift_id::text,'report_id',a.base_report_id::text,'reason',a.break_glass_reason,'at',coalesce(a.decided_at,a.requested_at))
      from public.amendments a
     where a.org_id=v_org_id and a.is_break_glass
    union all
    select jsonb_build_object('kind','loss_exception','source_id',x.exception_id::text,'shift_id',x.shift_id::text,'report_id',x.report_id::text,'loss_row_id',x.loss_row_id::text,'reason',x.reason,'at',x.created_at)
      from public.loss_exception x
     where x.org_id=v_org_id
    union all
    select jsonb_build_object('kind','variance','source_id',r.report_id::text,'shift_id',r.shift_id::text,'report_id',r.report_id::text,'version_no',r.version_no,'variance_rupiah',v.variance::text,'threshold',v.threshold::text,'at',r.submitted_at)
      from public.shift_reports r
      cross join lateral (
        select coalesce((select sum(d.expected_sale_rupiah) from public.dispenser_readings d where d.org_id=r.org_id and d.station_id=r.station_id and d.report_id=r.report_id),0)
             - coalesce((select sum(s.cash_amount+s.cashless_amount) from public.sales_declared s where s.org_id=r.org_id and s.station_id=r.station_id and s.report_id=r.report_id),0) variance,
               coalesce((select (i.payload->>'variance_rupiah_threshold')::numeric from public.policy_snapshot_items i where i.org_id=r.org_id and i.station_id=r.station_id and i.shift_id=r.shift_id and i.set_id=r.policy_snapshot_set_id and i.policy_kind='threshold'),0) threshold) v
     where r.org_id=v_org_id and abs(v.variance)>v.threshold
    order by 1;
end;
$$;
alter function public.read_anomalies() owner to report_writer;

create or replace function public.read_alerts()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  return query select jsonb_build_object('event_id',e.event_id::text,'rule_id',e.rule_id::text,'subject_kind',e.subject_kind::text,'subject_id',e.subject_id::text,'event_type',e.event_type::text,'period_start',e.period_start,'period_bucket',e.period_bucket,'related_fired_event_id',e.related_fired_event_id::text,'source_kind',e.source_kind::text,'source_id',e.source_id::text,'source_version_no',e.source_version_no,'source_at',e.source_at,'created_at',e.created_at)
    from public.alert_events e where e.org_id=v_org_id and (current_setting('app.station_id',true)='' or e.station_id::text=current_setting('app.station_id',true)) order by e.created_at desc;
end;
$$;
alter function public.read_alerts() owner to report_writer;

create or replace function public.read_ack_queue()
returns setof jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  return query select jsonb_build_object('shift_id',s.shift_id::text,'station_id',s.station_id::text,'report_id',s.current_report_id::text,'version_no',r.version_no,'submitted_by',r.submitted_by::text,'submitted_at',r.submitted_at)
    from public.shifts s join public.shift_reports r on r.org_id=s.org_id and r.station_id=s.station_id and r.shift_id=s.shift_id and r.report_id=s.current_report_id
   where s.org_id=v_org_id and s.status='awaiting_confirmation' and (current_setting('app.station_id',true)='' or s.station_id::text=current_setting('app.station_id',true)) order by r.submitted_at;
end;
$$;
alter function public.read_ack_queue() owner to report_writer;

alter table public.loss_entries enable row level security;
alter table public.loss_entries force row level security;

revoke all on function public.fn_evaluate_variance_alert(uuid), public.read_anomalies(), public.read_alerts(), public.read_ack_queue() from public, pomkita_app, report_writer, audit_owner, relay;
grant execute on function public.fn_evaluate_variance_alert(uuid) to pomkita_app, report_writer;
grant select on public.alert_rules to report_writer;
grant select on public.shift_reports, public.sales_declared to report_writer;
grant select on public.shift_reports to station_owner;
grant execute on function public.read_anomalies() to pomkita_app;
grant execute on function public.read_alerts() to pomkita_app;
grant execute on function public.read_ack_queue() to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_evaluate_variance_alert', array[]::text[], 'evaluate_variance_alert', 70),
  ('trg_report_variance_alert', array[]::text[], 'internal_trigger', 71),
  ('trg_amendment_evidence_clone', array[]::text[], 'internal_trigger', 51),
  ('read_anomalies', array['Station Admin','Owner','Superadmin'], 'read_anomalies', 53),
  ('read_alerts', array['Station Admin','Owner','Superadmin'], 'read_alerts', 71),
  ('read_ack_queue', array['Station Admin','Owner'], 'read_ack_queue', 52)
on conflict (name) do update set allowed_roles=excluded.allowed_roles, action=excluded.action, lock_rank=excluded.lock_rank;
