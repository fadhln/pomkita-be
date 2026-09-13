-- PLAN.md sections 4.2, 6, 7, and 8; BE-PLAN.md Phase B3.

create type public.alert_rule_type as enum ('starvation', 'variance');
create type public.alert_channel as enum ('in_app');
create type public.alert_subject_kind as enum ('shift', 'report');
create type public.alert_event_type as enum ('fired', 'cleared');
create type public.alert_source_kind as enum ('shift_transition', 'report', 'scheduler', 'amendment');

create table public.alert_rules (
  rule_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  rule_type public.alert_rule_type not null,
  alert_key text not null,
  threshold numeric not null,
  enabled boolean not null default true,
  channel public.alert_channel not null default 'in_app',
  created_by uuid,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, rule_id),
  unique (org_id, station_id, alert_key),
  foreign key (org_id, station_id) references public.stations(org_id, station_id),
  foreign key (org_id, created_by) references public.users(org_id, user_id),
  check (btrim(alert_key) <> ''),
  check (threshold >= 0),
  check (rule_type <> 'starvation' or threshold = 24)
);
alter table public.alert_rules owner to org_owner;

create table public.alert_events (
  event_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  rule_id uuid not null,
  subject_kind public.alert_subject_kind not null,
  subject_id uuid not null,
  event_type public.alert_event_type not null,
  period_start timestamptz(6) not null,
  period_bucket timestamptz(6) generated always as
    (date_bin(interval '1 hour', period_start, timestamptz '1970-01-01 00:00:00+00')) stored,
  related_fired_event_id uuid,
  source_kind public.alert_source_kind not null,
  source_id uuid not null,
  source_version_no integer,
  source_at timestamptz(6) not null,
  created_by uuid,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, event_id),
  unique (org_id, station_id, rule_id, subject_kind, subject_id, event_type, period_bucket),
  unique (org_id, station_id, related_fired_event_id, event_type),
  foreign key (org_id, station_id) references public.stations(org_id, station_id),
  foreign key (org_id, station_id, rule_id) references public.alert_rules(org_id, station_id, rule_id),
  foreign key (org_id, created_by) references public.users(org_id, user_id),
  foreign key (org_id, station_id, related_fired_event_id)
    references public.alert_events(org_id, station_id, event_id)
    deferrable initially deferred,
  check ((event_type = 'fired' and related_fired_event_id = event_id)
      or (event_type = 'cleared' and related_fired_event_id is not null)),
  check ((source_kind in ('scheduler', 'shift_transition') and source_version_no is null)
      or (source_kind in ('report', 'amendment') and source_version_no is not null)),
  check (source_version_no is null or source_version_no > 0)
);
alter table public.alert_events owner to station_owner;

create or replace function public.trg_alert_event_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  raise exception using errcode = '23514', message = 'alert_event_immutable';
end;
$$;
alter function public.trg_alert_event_guard() owner to station_owner;
create trigger alert_events_guard before update or delete on public.alert_events
for each row execute function public.trg_alert_event_guard();

create or replace function public.trg_alert_event_check()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_fired public.alert_events%rowtype;
  v_exists boolean;
begin
  select * into v_fired
    from public.alert_events e
   where e.org_id = new.org_id
     and e.station_id = new.station_id
     and e.event_id = new.related_fired_event_id;
  if new.event_type = 'cleared' and (not found or v_fired.event_type <> 'fired'
      or v_fired.rule_id <> new.rule_id or v_fired.subject_kind <> new.subject_kind
      or v_fired.subject_id <> new.subject_id or v_fired.period_bucket <> new.period_bucket) then
    raise exception using errcode = '23514', message = 'alert_clear_target_invalid';
  end if;

  if new.source_kind = 'shift_transition' then
    select exists (
      select 1 from public.shift_transitions t
       where t.org_id = new.org_id and t.station_id = new.station_id
         and t.transition_id = new.source_id) into v_exists;
  elsif new.source_kind = 'report' then
    select exists (
      select 1 from public.shift_reports r
       where r.org_id = new.org_id and r.station_id = new.station_id
         and r.report_id = new.source_id and r.version_no = new.source_version_no) into v_exists;
  elsif new.source_kind = 'amendment' then
    select exists (
      select 1 from public.amendments a
       where a.org_id = new.org_id and a.station_id = new.station_id
         and a.amendment_id = new.source_id) into v_exists;
  else
    select exists (
      select 1 from public.shifts s
       where s.org_id = new.org_id and s.station_id = new.station_id
         and s.shift_id = new.source_id) into v_exists;
  end if;
  if not v_exists then
    raise exception using errcode = '23514', message = 'alert_source_not_found';
  end if;
  return new;
end;
$$;
alter function public.trg_alert_event_check() owner to station_owner;
create constraint trigger alert_events_invariants after insert or update on public.alert_events
deferrable initially deferred for each row execute function public.trg_alert_event_check();

create or replace function public.fn_record_alert_occurrence(
  p_rule_id uuid,
  p_subject_kind public.alert_subject_kind,
  p_subject_id uuid,
  p_event_type public.alert_event_type,
  p_period_start timestamptz(6),
  p_source_kind public.alert_source_kind,
  p_source_id uuid,
  p_source_version_no integer,
  p_source_at timestamptz(6),
  p_created_by uuid default null,
  p_related_fired_event_id uuid default null
)
returns table (event_id uuid, inserted boolean)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_rule public.alert_rules%rowtype;
  v_org_id uuid;
  v_station_id uuid;
  v_related uuid := p_related_fired_event_id;
  v_event uuid := app.gen_random_uuid();
begin
  if current_setting('app.context_valid', true) <> 'true'
     and p_source_kind <> 'scheduler' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select r.* into v_rule
    from public.alert_rules r
   where r.rule_id = p_rule_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'alert_rule_not_found';
  end if;
  v_org_id := v_rule.org_id;
  v_station_id := v_rule.station_id;
  if not v_rule.enabled and p_event_type = 'fired' then
    event_id := null;
    inserted := false;
    return next;
    return;
  end if;

  if p_event_type = 'cleared' and v_related is null then
    select e.event_id into v_related
      from public.alert_events e
     where e.org_id = v_org_id and e.station_id = v_station_id
       and e.rule_id = p_rule_id and e.subject_kind = p_subject_kind
       and e.subject_id = p_subject_id and e.event_type = 'fired'
       and e.period_bucket = date_bin(interval '1 hour', p_period_start, timestamptz '1970-01-01 00:00:00+00')
     order by e.created_at desc
     limit 1
     for update;
    if v_related is null then
      event_id := null;
      inserted := false;
      return next;
      return;
    end if;
  end if;

  insert into public.alert_events(
    event_id, org_id, station_id, rule_id, subject_kind, subject_id,
    event_type, period_start, related_fired_event_id, source_kind,
    source_id, source_version_no, source_at, created_by)
  values (
    case when p_event_type = 'fired' then v_event else app.gen_random_uuid() end,
    v_org_id, v_station_id, p_rule_id, p_subject_kind, p_subject_id,
    p_event_type, p_period_start,
    case when p_event_type = 'fired' then v_event else v_related end,
    p_source_kind, p_source_id, p_source_version_no, p_source_at, p_created_by)
  on conflict (org_id, station_id, rule_id, subject_kind, subject_id, event_type, period_bucket)
  do nothing
  returning public.alert_events.event_id into event_id;
  inserted := event_id is not null;
  if not inserted then
    select e.event_id into event_id
      from public.alert_events e
     where e.org_id = v_org_id and e.station_id = v_station_id
       and e.rule_id = p_rule_id and e.subject_kind = p_subject_kind
       and e.subject_id = p_subject_id and e.event_type = p_event_type
       and e.period_bucket = date_bin(interval '1 hour', p_period_start, timestamptz '1970-01-01 00:00:00+00')
     limit 1;
  end if;
  return next;
end;
$$;
alter function public.fn_record_alert_occurrence(uuid,public.alert_subject_kind,uuid,public.alert_event_type,timestamptz,public.alert_source_kind,uuid,integer,timestamptz,uuid,uuid) owner to station_owner;

create or replace function public.fn_run_starvation_alerts(p_now timestamptz(6) default clock_timestamp())
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_row record;
  v_result record;
  v_count integer := 0;
begin
  for v_row in
    select r.rule_id, r.org_id, r.station_id, s.shift_id, s.opened_at
      from public.alert_rules r
      join public.shifts s on s.org_id = r.org_id and s.station_id = r.station_id
     where r.rule_type = 'starvation' and r.enabled
       and s.status in ('open', 'awaiting_confirmation')
       and s.opened_at <= p_now - interval '24 hours'
     order by r.org_id, r.station_id, s.station_seq
  loop
    perform set_config('app.context_valid', 'true', true);
    perform set_config('app.org_id', v_row.org_id::text, true);
    perform set_config('app.station_id', v_row.station_id::text, true);
    perform set_config('app.user_id', '', true);
    perform set_config('app.role', '', true);
    select * into v_result from public.fn_record_alert_occurrence(
      v_row.rule_id, 'shift', v_row.shift_id, 'fired', v_row.opened_at,
      'scheduler', v_row.shift_id, null, p_now, null, null);
    if v_result.inserted then
      v_count := v_count + 1;
    end if;
  end loop;
  return v_count;
end;
$$;
alter function public.fn_run_starvation_alerts(timestamptz) owner to station_owner;

create or replace function public.fn_transition_shift(
  p_shift_id uuid,
  p_to_status public.shift_status,
  p_reason text default null
)
returns void
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_from public.shift_status;
  v_opened_at timestamptz(6);
  v_transition_id uuid;
  v_rule record;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select station_id into v_station_id from public.shifts
   where org_id = v_org_id and shift_id = p_shift_id;
  if not found then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations where org_id = v_org_id and station_id = v_station_id for update;
  select status, opened_at into v_from, v_opened_at from public.shifts
   where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id for update;
  if not ((v_from, p_to_status) in (
    ('open', 'submitting'), ('submitting', 'awaiting_confirmation'), ('submitting', 'failed'),
    ('failed', 'open'), ('failed', 'abandoned'), ('awaiting_confirmation', 'locked'),
    ('awaiting_confirmation', 'needs_correction'), ('needs_correction', 'awaiting_confirmation'),
    ('locked', 'awaiting_confirmation')
  )) then
    raise exception using errcode = '23514', message = 'invalid_shift_transition';
  end if;
  perform set_config('app.transition', 'shift_transition', true);
  update public.shifts
     set status = p_to_status,
         closed_at = case when p_to_status in ('locked', 'abandoned') then clock_timestamp() else closed_at end
   where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  insert into public.shift_transitions(org_id, station_id, shift_id, from_status, to_status, actor_user_id, reason)
  values (v_org_id, v_station_id, p_shift_id, v_from, p_to_status,
          nullif(current_setting('app.user_id', true), '')::uuid, p_reason)
  returning transition_id into v_transition_id;
  if p_to_status = 'open' then
    update public.shift_drafts set status = 'editing', revision = revision + 1, updated_at = clock_timestamp()
     where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  elsif p_to_status = 'submitting' then
    update public.shift_drafts set status = 'submitting', updated_at = clock_timestamp()
     where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  elsif p_to_status = 'failed' then
    update public.shift_drafts set status = 'failed', updated_at = clock_timestamp()
     where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  elsif p_to_status = 'awaiting_confirmation' then
    update public.shift_drafts set status = 'submitted', updated_at = clock_timestamp()
     where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  end if;
  if p_to_status = 'locked' then
    for v_rule in
      select rule_id from public.alert_rules
       where org_id = v_org_id and station_id = v_station_id and rule_type = 'starvation'
    loop
      perform * from public.fn_record_alert_occurrence(
        v_rule.rule_id, 'shift', p_shift_id, 'cleared', v_opened_at,
        'shift_transition', v_transition_id, null, clock_timestamp(),
        nullif(current_setting('app.user_id', true), '')::uuid, null);
    end loop;
  end if;
end;
$$;
alter function public.fn_transition_shift(uuid,public.shift_status,text) owner to station_owner;

alter table public.alert_rules enable row level security;
alter table public.alert_rules force row level security;
alter table public.alert_events enable row level security;
alter table public.alert_events force row level security;
create policy alert_rules_context on public.alert_rules
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy alert_events_context on public.alert_events
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy alert_rules_scheduler on public.alert_rules to station_owner using (true) with check (true);
create policy alert_events_scheduler on public.alert_events to station_owner using (true) with check (true);
create policy alert_rules_scheduler_job on public.alert_rules to report_writer using (true) with check (true);
create policy alert_shifts_scheduler on public.shifts to station_owner using (true) with check (true);

revoke all on table public.alert_rules, public.alert_events from public, pomkita_app, report_writer, audit_owner, relay;
grant select, update on public.alert_rules to station_owner;
grant select on public.alert_events to station_owner, report_writer;
grant insert on public.alert_events to station_owner;
grant insert on public.outbox_relay_state to audit_owner;
grant execute on function public.fn_record_alert_occurrence(uuid,public.alert_subject_kind,uuid,public.alert_event_type,timestamptz,public.alert_source_kind,uuid,integer,timestamptz,uuid,uuid) to pomkita_app;
grant execute on function public.fn_run_starvation_alerts(timestamptz) to station_owner;
grant execute on function public.fn_transition_shift(uuid,public.shift_status,text) to pomkita_app, report_writer;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_record_alert_occurrence', array[]::text[], 'record_alert_occurrence', 70),
  ('fn_run_starvation_alerts', array[]::text[], 'run_starvation_alerts', 70),
  ('trg_alert_event_guard', array[]::text[], 'internal_trigger', 71),
  ('trg_alert_event_check', array[]::text[], 'internal_trigger', 71)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
