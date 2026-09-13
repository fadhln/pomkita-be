-- PLAN.md sections 4.3, 7, 8, and 9; BE-PLAN.md Phase B2.

create table public.audit_log (
  event_id uuid primary key,
  org_id uuid not null,
  org_sequence bigint not null,
  event_type text not null,
  payload jsonb not null,
  outcome text not null,
  outcome_error text,
  created_at timestamptz(6) not null,
  prev_hash bytea not null check (octet_length(prev_hash) = 32),
  row_hash bytea not null check (octet_length(row_hash) = 32),
  unique (org_id, event_id),
  unique (org_id, org_sequence),
  foreign key (org_id) references public.organizations(org_id),
  check (org_sequence > 0 and btrim(event_type) <> '' and btrim(outcome) <> '')
);
alter table public.audit_log owner to audit_owner;

create table public.audit_outbox (
  org_id uuid not null,
  event_id uuid not null,
  event_type text not null,
  payload jsonb not null,
  created_at timestamptz(6) not null,
  primary key (org_id, event_id),
  foreign key (org_id, event_id) references public.audit_log(org_id, event_id)
);
alter table public.audit_outbox owner to audit_owner;

create table public.audit_denied (
  request_id uuid primary key,
  sub uuid,
  jti uuid,
  org_id uuid,
  station_id uuid,
  action text not null,
  target text not null,
  reason text not null,
  server_at timestamptz(6) not null default clock_timestamp(),
  outcome text not null,
  error_detail text,
  check (btrim(action) <> '' and btrim(target) <> '' and btrim(reason) <> '' and btrim(outcome) <> '')
);
alter table public.audit_denied owner to audit_owner;

create or replace function public.trg_audit_append_only()
returns trigger language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op <> 'INSERT' then
    raise exception using errcode = '23514', message = 'audit_immutable';
  end if;
  return new;
end;
$$;
alter function public.trg_audit_append_only() owner to audit_owner;
create trigger audit_outbox_guard before update or delete on public.audit_outbox
for each row execute function public.trg_audit_append_only();
create trigger audit_denied_guard before update or delete on public.audit_denied
for each row execute function public.trg_audit_append_only();

create or replace function public.fn_append_audit_event(
  p_event_id uuid, p_event_type text, p_payload jsonb, p_outcome text,
  p_outcome_error text default null
)
returns table (event_id uuid, org_sequence bigint, row_hash bytea)
language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_sequence bigint;
  v_prev_hash bytea;
  v_created_at timestamptz(6) := clock_timestamp();
  v_hash bytea;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if p_event_id is null or btrim(coalesce(p_event_type, '')) = ''
     or p_payload is null or btrim(coalesce(p_outcome, '')) = '' then
    raise exception using errcode = '22023', message = 'invalid_audit_event';
  end if;
  insert into public.audit_chain_locks(org_id) values (v_org_id) on conflict (org_id) do nothing;
  perform 1 from public.audit_chain_locks where org_id = v_org_id for update;
  select coalesce(max(a.org_sequence), 0) + 1,
         coalesce((select x.row_hash from public.audit_log x where x.org_id = v_org_id order by x.org_sequence desc limit 1), decode(repeat('00', 32), 'hex'))
    into v_sequence, v_prev_hash
    from public.audit_log a where a.org_id = v_org_id;
  v_hash := app.digest(convert_to(jsonb_build_array(
    1, v_org_id::text, v_sequence::text, lower(p_event_id::text), p_event_type,
    p_payload, to_char(v_created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    encode(v_prev_hash, 'hex'))::text, 'UTF8'), 'sha256');
  perform set_config('app.transition', 'audit_append', true);
  insert into public.audit_log(event_id, org_id, org_sequence, event_type, payload,
    outcome, outcome_error, created_at, prev_hash, row_hash)
  values (p_event_id, v_org_id, v_sequence, p_event_type, p_payload,
    p_outcome, p_outcome_error, v_created_at, v_prev_hash, v_hash);
  insert into public.audit_outbox(org_id, event_id, event_type, payload, created_at)
  values (v_org_id, p_event_id, p_event_type, p_payload, v_created_at);
  event_id := p_event_id;
  org_sequence := v_sequence;
  row_hash := v_hash;
  return next;
end;
$$;
alter function public.fn_append_audit_event(uuid, text, jsonb, text, text) owner to audit_owner;

create or replace function public.read_audit_chain()
returns table (event_id uuid, org_sequence bigint, event_type text, payload jsonb,
  outcome text, outcome_error text, created_at timestamptz(6), prev_hash bytea, row_hash bytea)
language sql security definer
set search_path = pg_catalog, public, app
as $function$
  select a.event_id, a.org_sequence, a.event_type, a.payload, a.outcome,
         a.outcome_error, a.created_at, a.prev_hash, a.row_hash
    from public.audit_log a
   where a.org_id = nullif(current_setting('app.org_id', true), '')::uuid
   order by a.org_sequence
$function$;
alter function public.read_audit_chain() owner to audit_owner;

create or replace function public.fn_verify_audit_chain()
returns boolean language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_expected_sequence bigint := 1;
  v_prev_hash bytea := decode(repeat('00', 32), 'hex');
  v_expected_hash bytea;
  v record;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  for v in select * from public.audit_log where org_id = v_org_id order by org_sequence loop
    if v.org_sequence <> v_expected_sequence or v.prev_hash <> v_prev_hash then
      raise exception using errcode = '23514', message = 'audit_chain_tampered';
    end if;
    v_expected_hash := app.digest(convert_to(jsonb_build_array(
      1, v_org_id::text, v.org_sequence::text, lower(v.event_id::text), v.event_type,
      v.payload, to_char(v.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      encode(v.prev_hash, 'hex'))::text, 'UTF8'), 'sha256');
    if v.row_hash <> v_expected_hash then
      raise exception using errcode = '23514', message = 'audit_chain_tampered';
    end if;
    v_prev_hash := v.row_hash;
    v_expected_sequence := v_expected_sequence + 1;
  end loop;
  return true;
end;
$$;
alter function public.fn_verify_audit_chain() owner to audit_owner;

create or replace function public.trg_governance_audit()
returns trigger language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_transition text := current_setting('app.transition', true);
  v_org_id uuid;
  v_event_id uuid;
  v_type text;
  v_payload jsonb;
begin
  if tg_table_name = 'shift_transitions' then
    v_org_id := new.org_id;
    v_event_id := new.transition_id;
    v_type := 'shift_transition';
    v_payload := jsonb_build_object('shift_id', new.shift_id::text, 'from_status', new.from_status::text, 'to_status', new.to_status::text, 'reason', new.reason);
  elsif tg_table_name = 'ack_decisions' then
    v_org_id := new.org_id;
    v_event_id := new.ack_id;
    v_type := 'ack_decision';
    v_payload := jsonb_build_object('shift_id', new.shift_id::text, 'report_id', new.report_id::text, 'version_no', new.version_no, 'decision', new.decision::text, 'is_break_glass', new.is_break_glass);
  elsif tg_table_name = 'amendments' and new.status = 'approved' then
    v_org_id := new.org_id;
    v_event_id := new.amendment_id;
    v_type := 'amendment_approved';
    v_payload := jsonb_build_object('shift_id', new.shift_id::text, 'base_report_id', new.base_report_id::text, 'applied_report_id', new.applied_report_id::text);
  else
    return new;
  end if;
  perform public.fn_append_audit_event(v_event_id, v_type, v_payload, 'success', null);
  perform set_config('app.transition', v_transition, true);
  return new;
end;
$$;
alter function public.trg_governance_audit() owner to audit_owner;
create trigger shift_transitions_audit after insert on public.shift_transitions
for each row execute function public.trg_governance_audit();
create trigger ack_decisions_audit after insert on public.ack_decisions
for each row execute function public.trg_governance_audit();
create trigger amendments_audit after update on public.amendments
for each row execute function public.trg_governance_audit();

create or replace function public.fn_record_audit_denied(
  p_request_id uuid, p_sub uuid, p_jti uuid, p_org_id uuid, p_station_id uuid,
  p_action text, p_target text, p_reason text, p_outcome text,
  p_error_detail text default null
)
returns void language sql security definer
set search_path = pg_catalog, public, app
as $function$
  insert into public.audit_denied(request_id, sub, jti, org_id, station_id, action,
    target, reason, outcome, error_detail)
  values (p_request_id, p_sub, p_jti, p_org_id, p_station_id, p_action,
    p_target, p_reason, p_outcome, p_error_detail)
$function$;
alter function public.fn_record_audit_denied(uuid, uuid, uuid, uuid, uuid, text, text, text, text, text) owner to audit_owner;

grant usage on schema public, app to audit_owner;
grant insert, select, update on public.audit_chain_locks to audit_owner;
grant insert, select on public.audit_log, public.audit_outbox, public.audit_denied to audit_owner;
grant execute on function public.fn_append_audit_event(uuid, text, jsonb, text, text) to report_writer, station_owner;
grant execute on function public.fn_record_audit_denied(uuid, uuid, uuid, uuid, uuid, text, text, text, text, text) to pomkita_app;
grant execute on function public.read_audit_chain() to pomkita_app;
grant execute on function public.fn_verify_audit_chain() to pomkita_app;

do $$
declare v_table text;
begin
  foreach v_table in array array['audit_log', 'audit_outbox', 'audit_denied'] loop
    execute format('alter table public.%I enable row level security', v_table);
    execute format('alter table public.%I force row level security', v_table);
  end loop;
end
$$;
create policy audit_log_context on public.audit_log using (org_id::text = current_setting('app.org_id', true));
create policy audit_outbox_context on public.audit_outbox using (org_id::text = current_setting('app.org_id', true));
create policy audit_outbox_relay on public.audit_outbox to relay
using (org_id::text = coalesce(nullif(current_setting('app.relay_org_id', true), ''), current_setting('app.org_id', true)));
create policy audit_denied_context on public.audit_denied using (org_id::text = current_setting('app.org_id', true));
create policy audit_denied_writer on public.audit_denied to audit_owner using (true) with check (true);

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_append_audit_event', array[]::text[], 'append_audit_event', 100),
  ('fn_record_audit_denied', array[]::text[], 'record_audit_denied', 100),
  ('read_audit_chain', array['Station Admin', 'Owner', 'Superadmin'], 'read_audit_chain', 100),
  ('fn_verify_audit_chain', array['Station Admin', 'Owner', 'Superadmin'], 'verify_audit_chain', 100),
  ('trg_audit_append_only', array[]::text[], 'internal_trigger', 100),
  ('trg_governance_audit', array[]::text[], 'internal_trigger', 100)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
