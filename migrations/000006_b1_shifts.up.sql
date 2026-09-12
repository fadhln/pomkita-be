-- PLAN.md sections 3.1, 4.2, 4.3, 5.1, 5.2, 7, and 8; BE-PLAN.md Phase B1.

create type public.shift_status as enum
  ('open', 'submitting', 'failed', 'abandoned', 'awaiting_confirmation', 'needs_correction', 'locked');
create type public.draft_status as enum
  ('editing', 'submitting', 'submitted', 'failed', 'recovering');

create table public.shifts (
  shift_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  station_seq bigint not null,
  supervisor_id uuid not null,
  opened_at timestamptz(6) not null,
  closed_at timestamptz(6),
  timezone_snapshot text not null,
  business_date date not null,
  original_event_date date,
  shift_ke integer,
  backfilled boolean not null default false,
  backfill_approver uuid,
  backfill_approved_at timestamptz(6),
  backfill_reason text,
  status public.shift_status not null default 'open',
  current_report_id uuid,
  shift_price_map_snapshot jsonb not null,
  shift_price_map_hash bytea not null check (octet_length(shift_price_map_hash) = 32),
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, shift_id),
  unique (org_id, station_id, station_seq),
  foreign key (org_id, station_id) references public.stations (org_id, station_id),
  foreign key (org_id, supervisor_id) references public.users (org_id, user_id),
  foreign key (org_id, backfill_approver) references public.users (org_id, user_id),
  check (station_seq > 0),
  check (shift_ke is null or shift_ke > 0),
  check ((backfilled = false and original_event_date is null and shift_ke is null and backfill_approver is null and backfill_approved_at is null and backfill_reason is null)
      or (backfilled = true and original_event_date is not null and shift_ke is not null and backfill_approver is not null and backfill_approved_at is not null and btrim(coalesce(backfill_reason, '')) <> '')),
  check (closed_at is null or closed_at >= opened_at)
);
alter table public.shifts owner to station_owner;
create unique index shifts_one_active_per_station
  on public.shifts (org_id, station_id)
  where status in ('open', 'submitting', 'failed', 'awaiting_confirmation');
create unique index shifts_backfill_event_key
  on public.shifts (org_id, station_id, original_event_date, shift_ke)
  where backfilled;

create table public.shift_transitions (
  transition_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  from_status public.shift_status not null,
  to_status public.shift_status not null,
  actor_user_id uuid,
  at timestamptz(6) not null default clock_timestamp(),
  reason text,
  unique (org_id, station_id, transition_id),
  foreign key (org_id, station_id, shift_id) references public.shifts (org_id, station_id, shift_id),
  foreign key (org_id, actor_user_id) references public.users (org_id, user_id)
);
alter table public.shift_transitions owner to station_owner;

create table public.shift_drafts (
  draft_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  owned_by uuid,
  claim_token uuid,
  claim_expires_at timestamptz(6),
  status public.draft_status not null default 'editing',
  updated_by uuid,
  updated_at timestamptz(6) not null default clock_timestamp(),
  revision integer not null default 1,
  recovery_count integer not null default 0,
  unique (org_id, station_id, shift_id),
  unique (org_id, station_id, draft_id),
  foreign key (org_id, station_id, shift_id) references public.shifts (org_id, station_id, shift_id),
  foreign key (org_id, owned_by) references public.users (org_id, user_id),
  foreign key (org_id, updated_by) references public.users (org_id, user_id),
  check (revision > 0),
  check (recovery_count >= 0),
  check ((claim_token is null and claim_expires_at is null) or (claim_token is not null and claim_expires_at is not null))
);
alter table public.shift_drafts owner to station_owner;

create table public.draft_readings (
  row_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  draft_id uuid not null,
  nozzle_id uuid not null,
  meter_start numeric(10,1) not null,
  meter_end numeric(10,1) not null,
  created_by uuid not null,
  unique (org_id, station_id, draft_id, nozzle_id),
  foreign key (org_id, station_id, draft_id) references public.shift_drafts (org_id, station_id, draft_id),
  foreign key (org_id, station_id, nozzle_id) references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, created_by) references public.users (org_id, user_id),
  check (meter_start >= 0 and meter_end >= 0)
);
alter table public.draft_readings owner to station_owner;

create table public.draft_sales (
  row_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  draft_id uuid not null,
  dispenser_id uuid not null,
  cash_amount numeric(14,0) not null,
  cashless_amount numeric(14,0) not null default 0,
  created_by uuid not null,
  unique (org_id, station_id, draft_id, dispenser_id),
  foreign key (org_id, station_id, draft_id) references public.shift_drafts (org_id, station_id, draft_id),
  foreign key (org_id, station_id, dispenser_id) references public.dispensers (org_id, station_id, dispenser_id),
  foreign key (org_id, created_by) references public.users (org_id, user_id),
  check (cash_amount >= 0 and cashless_amount >= 0)
);
alter table public.draft_sales owner to station_owner;

create table public.draft_losses (
  row_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  draft_id uuid not null,
  loss_id uuid not null,
  direction text not null,
  reason_code text not null,
  liters numeric(8,2) not null,
  cash_amount numeric(14,0),
  note text,
  created_by uuid not null,
  unique (org_id, station_id, draft_id, loss_id),
  foreign key (org_id, station_id, draft_id) references public.shift_drafts (org_id, station_id, draft_id),
  foreign key (org_id, created_by) references public.users (org_id, user_id),
  check (direction in ('loss', 'gain')),
  check (liters >= 0),
  check (cash_amount is null or cash_amount >= 0)
);
alter table public.draft_losses owner to station_owner;

create table public.draft_evidence_staging (
  row_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  draft_id uuid not null,
  loss_row_id uuid not null,
  evidence_type text not null,
  object_key text not null,
  content_hash bytea not null check (octet_length(content_hash) = 32),
  size_bytes bigint not null check (size_bytes > 0),
  mime text not null,
  status text not null default 'uploaded',
  uploaded_by uuid not null,
  foreign key (org_id, station_id, draft_id) references public.shift_drafts (org_id, station_id, draft_id),
  foreign key (org_id, uploaded_by) references public.users (org_id, user_id),
  check (btrim(evidence_type) <> '' and btrim(object_key) <> ''),
  check (status in ('uploaded', 'verified', 'finalized'))
);
alter table public.draft_evidence_staging owner to station_owner;

create table public.submit_idempotency (
  idem_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  idempotency_key text not null,
  request_hash bytea not null check (octet_length(request_hash) = 32),
  status text not null default 'in_progress',
  claim_token uuid not null,
  attempt_count integer not null default 1,
  lease_started_at timestamptz(6) not null default clock_timestamp(),
  lease_expires_at timestamptz(6) not null,
  resulting_report_id uuid,
  error_detail text,
  created_at timestamptz(6) not null default clock_timestamp(),
  updated_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, shift_id, idempotency_key),
  foreign key (org_id, station_id, shift_id) references public.shifts (org_id, station_id, shift_id),
  check (btrim(idempotency_key) <> ''),
  check (status in ('in_progress', 'succeeded', 'failed')),
  check (attempt_count > 0),
  check (lease_expires_at >= lease_started_at)
);
alter table public.submit_idempotency owner to station_owner;

alter table public.meter_reset_events
  add constraint meter_reset_events_effective_shift_fk
  foreign key (org_id, station_id, effective_shift_id)
  references public.shifts (org_id, station_id, shift_id);

create or replace function public.fn_open_shift(
  p_station_id uuid,
  p_supervisor_id uuid,
  p_opened_at timestamptz,
  p_backfilled boolean,
  p_original_event_date date,
  p_shift_ke integer,
  p_backfill_approver uuid,
  p_backfill_reason text
)
returns table (shift_id uuid, station_seq bigint, business_date date, shift_price_map_snapshot jsonb, shift_price_map_hash bytea)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_timezone text;
  v_seq bigint;
  v_snapshot jsonb;
  v_hash bytea;
  v_shift_id uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_org_id is null or p_station_id is null or p_supervisor_id is null or p_opened_at is null then
    raise exception using errcode = '22023', message = 'invalid_open_shift_request';
  end if;
  if current_setting('app.user_id', true)::uuid <> p_supervisor_id then
    raise exception using errcode = '42501', message = 'supervisor_identity_mismatch';
  end if;
  select timezone into v_timezone
    from public.stations
   where org_id = v_org_id and station_id = p_station_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'station_not_found';
  end if;
  select coalesce(max(s.station_seq), 0) + 1 into v_seq
    from public.shifts s
   where s.org_id = v_org_id and s.station_id = p_station_id;

  select jsonb_build_object(
           'hash_version', 1,
           'items', jsonb_agg(jsonb_build_object(
             'dispenser_id', n.dispenser_id::text,
             'nozzle_id', n.nozzle_id::text,
             'meter_max', n.meter_max::text,
             'modulus', (n.meter_max + 0.1)::text,
             'price', p.price::text,
             'price_id', p.price_id::text,
             'dispenser_nozzle_map_id', dnm.map_id::text,
             'nozzle_tank_map_id', ntm.map_id::text
           ) order by n.nozzle_id)
         )
    into v_snapshot
    from public.nozzles n
    join public.dispenser_prices p
      on p.org_id = n.org_id and p.station_id = n.station_id and p.nozzle_id = n.nozzle_id
     and p.valid_period @> p_opened_at
    join public.dispenser_nozzle_map dnm
      on dnm.org_id = n.org_id and dnm.station_id = n.station_id and dnm.nozzle_id = n.nozzle_id
     and dnm.valid_period @> p_opened_at
    left join public.nozzle_tank_map ntm
      on ntm.org_id = n.org_id and ntm.station_id = n.station_id and ntm.nozzle_id = n.nozzle_id
     and ntm.valid_period @> p_opened_at
   where n.org_id = v_org_id and n.station_id = p_station_id;
  if v_snapshot is null or coalesce(jsonb_array_length(v_snapshot->'items'), 0) = 0 then
    raise exception using errcode = '23514', message = 'catalog_snapshot_incomplete';
  end if;
  if p_backfilled and (p_original_event_date is null or p_shift_ke is null or p_backfill_approver is null or btrim(coalesce(p_backfill_reason, '')) = '') then
    raise exception using errcode = '23514', message = 'backfill_approval_required';
  end if;
  if not p_backfilled and (p_original_event_date is not null or p_shift_ke is not null or p_backfill_approver is not null or p_backfill_reason is not null) then
    raise exception using errcode = '23514', message = 'unexpected_backfill_fields';
  end if;
  v_hash := digest(v_snapshot::text, 'sha256');
  v_shift_id := app.gen_random_uuid();
  insert into public.shifts (
    shift_id, org_id, station_id, station_seq, supervisor_id, opened_at, timezone_snapshot,
    business_date, original_event_date, shift_ke, backfilled, backfill_approver,
    backfill_approved_at, backfill_reason, shift_price_map_snapshot, shift_price_map_hash
  ) values (
    v_shift_id, v_org_id, p_station_id, v_seq, p_supervisor_id, p_opened_at, v_timezone,
    (p_opened_at at time zone v_timezone)::date, p_original_event_date, p_shift_ke,
    p_backfilled, p_backfill_approver, case when p_backfilled then clock_timestamp() end,
    p_backfill_reason, v_snapshot, v_hash
  );
  insert into public.shift_drafts (org_id, station_id, shift_id, owned_by, updated_by)
  values (v_org_id, p_station_id, v_shift_id, p_supervisor_id, p_supervisor_id);
  return query select v_shift_id, v_seq, (p_opened_at at time zone v_timezone)::date, v_snapshot, v_hash;
end;
$$;
alter function public.fn_open_shift(uuid, uuid, timestamptz, boolean, date, integer, uuid, text) owner to station_owner;

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
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select station_id into v_station_id from public.shifts where org_id = v_org_id and shift_id = p_shift_id;
  if not found then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations where org_id = v_org_id and station_id = v_station_id for update;
  select status into v_from from public.shifts where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id for update;
  if not ((v_from, p_to_status) in (
    ('open', 'submitting'), ('submitting', 'awaiting_confirmation'), ('submitting', 'failed'),
    ('failed', 'open'), ('failed', 'abandoned'), ('awaiting_confirmation', 'locked'),
    ('awaiting_confirmation', 'needs_correction'), ('needs_correction', 'awaiting_confirmation'),
    ('locked', 'awaiting_confirmation')
  )) then
    raise exception using errcode = '23514', message = 'invalid_shift_transition';
  end if;
  perform set_config('app.transition', 'shift_transition', true);
  update public.shifts set status = p_to_status, closed_at = case when p_to_status in ('locked', 'abandoned') then clock_timestamp() else closed_at end
   where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  insert into public.shift_transitions (org_id, station_id, shift_id, from_status, to_status, actor_user_id, reason)
  values (v_org_id, v_station_id, p_shift_id, v_from, p_to_status, nullif(current_setting('app.user_id', true), '')::uuid, p_reason);
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
end;
$$;
alter function public.fn_transition_shift(uuid, public.shift_status, text) owner to station_owner;

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
    if current_setting('app.transition', true) <> 'shift_transition' then
      raise exception using errcode = '23514', message = 'transition_insert_forbidden';
    end if;
    return new;
  end if;
  if new.station_seq <> old.station_seq or new.shift_price_map_snapshot <> old.shift_price_map_snapshot or new.shift_price_map_hash <> old.shift_price_map_hash then
    raise exception using errcode = '23514', message = 'shift_immutable_fields';
  end if;
  if new.status <> old.status and current_setting('app.transition', true) <> 'shift_transition' then
    raise exception using errcode = '23514', message = 'shift_transition_required';
  end if;
  return new;
end;
$$;
alter function public.trg_shift_guard() owner to station_owner;
create trigger shifts_guard before update or delete on public.shifts for each row execute function public.trg_shift_guard();
create trigger shift_transitions_insert_guard before insert or update or delete on public.shift_transitions for each row execute function public.trg_shift_guard();

do $$
declare
  v_table text;
begin
  foreach v_table in array array[
    'shifts', 'shift_transitions', 'shift_drafts', 'draft_readings', 'draft_sales',
    'draft_losses', 'draft_evidence_staging', 'submit_idempotency'
  ] loop
    execute format('alter table public.%I enable row level security', v_table);
    execute format('alter table public.%I force row level security', v_table);
    execute format(
      'create policy %I_context on public.%I using (org_id::text = current_setting(''app.org_id'', true) and (current_setting(''app.station_id'', true) = '''' or station_id::text = current_setting(''app.station_id'', true))) with check (org_id::text = current_setting(''app.org_id'', true) and (current_setting(''app.station_id'', true) = '''' or station_id::text = current_setting(''app.station_id'', true)))',
      v_table, v_table
    );
  end loop;
end
$$;

revoke all on table public.shifts, public.shift_transitions, public.shift_drafts,
  public.draft_readings, public.draft_sales, public.draft_losses,
  public.draft_evidence_staging, public.submit_idempotency
  from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on all sequences in schema public from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on all functions in schema public from public, pomkita_app, report_writer, audit_owner, relay;
grant execute on function public.fn_open_shift(uuid, uuid, timestamptz, boolean, date, integer, uuid, text) to pomkita_app;
grant execute on function public.fn_transition_shift(uuid, public.shift_status, text) to pomkita_app;

insert into public.procedure_registry (name, allowed_roles, action, lock_rank)
values
  ('fn_open_shift', array['Supervisor', 'Owner'], 'open_shift', 10),
  ('fn_transition_shift', array['Supervisor', 'Station Admin', 'Owner'], 'transition_shift', 20),
  ('trg_shift_guard', array[]::text[], 'internal_trigger', 20);
