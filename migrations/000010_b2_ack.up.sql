-- PLAN.md sections 3.2, 3.4, 4.2, 4.3, 7, and 8; BE-PLAN.md Phase B2.

create type public.ack_decision as enum ('acked', 'rejected');

create table public.ack_decisions (
  ack_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  report_id uuid not null,
  version_no integer not null,
  ack_seq bigint not null,
  decision public.ack_decision not null,
  actor_user_id uuid not null,
  decided_at timestamptz(6) not null default clock_timestamp(),
  rejection_reason text,
  is_superadmin boolean not null default false,
  is_break_glass boolean not null default false,
  break_glass_reason text,
  unique (org_id, station_id, shift_id, report_id, version_no, ack_id),
  unique (org_id, station_id, shift_id, report_id, version_no, ack_seq),
  foreign key (org_id, station_id, shift_id, report_id, version_no)
    references public.shift_reports (org_id, station_id, shift_id, report_id, version_no),
  foreign key (org_id, actor_user_id) references public.users (org_id, user_id),
  check (ack_seq > 0),
  check ((decision = 'rejected' and btrim(coalesce(rejection_reason, '')) <> '')
      or (decision = 'acked' and rejection_reason is null)),
  check ((is_break_glass and btrim(coalesce(break_glass_reason, '')) <> '')
      or (not is_break_glass and break_glass_reason is null))
);
alter table public.ack_decisions owner to report_writer;

create table public.ack_head (
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  report_id uuid not null,
  version_no integer not null,
  active_ack_id uuid,
  primary key (org_id, station_id, shift_id, report_id, version_no),
  foreign key (org_id, station_id, shift_id, report_id, version_no)
    references public.shift_reports (org_id, station_id, shift_id, report_id, version_no),
  foreign key (org_id, station_id, shift_id, report_id, version_no, active_ack_id)
    references public.ack_decisions (org_id, station_id, shift_id, report_id, version_no, ack_id)
    deferrable initially deferred
);
alter table public.ack_head owner to report_writer;

create table public.ack_supersessions (
  supersession_id uuid primary key default app.gen_random_uuid(),
  old_org_id uuid not null,
  old_station_id uuid not null,
  old_shift_id uuid not null,
  old_report_id uuid not null,
  old_version_no integer not null,
  superseded_ack_id uuid not null,
  replacement_org_id uuid not null,
  replacement_station_id uuid not null,
  replacement_shift_id uuid not null,
  replacement_report_id uuid not null,
  replacement_version_no integer not null,
  reason text not null,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id),
  foreign key (old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id)
    references public.ack_decisions (org_id, station_id, shift_id, report_id, version_no, ack_id),
  foreign key (replacement_org_id, replacement_station_id, replacement_shift_id, replacement_report_id)
    references public.shift_reports (org_id, station_id, shift_id, report_id),
  check (replacement_version_no = old_version_no + 1),
  check (btrim(reason) <> '')
);
alter table public.ack_supersessions owner to report_writer;

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
    if current_setting('app.transition', true) <> 'submit_shift' then
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

create trigger ack_decisions_guard before update or delete on public.ack_decisions
for each row execute function public.trg_ack_guard();
create trigger ack_head_guard before insert or update or delete on public.ack_head
for each row execute function public.trg_ack_guard();
create trigger ack_supersessions_guard before update or delete on public.ack_supersessions
for each row execute function public.trg_ack_guard();

create or replace function public.trg_ack_report_head()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if current_setting('app.transition', true) = 'submit_shift' then
    insert into public.ack_head(org_id, station_id, shift_id, report_id, version_no)
    values (new.org_id, new.station_id, new.shift_id, new.report_id, new.version_no);
  end if;
  return new;
end;
$$;
alter function public.trg_ack_report_head() owner to report_writer;
create trigger shift_reports_ack_head after insert on public.shift_reports
for each row execute function public.trg_ack_report_head();

create or replace function public.trg_ack_cardinality()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid;
  v_station_id uuid;
  v_shift_id uuid;
  v_report_id uuid;
  v_version_no integer;
  v_status text;
  v_active integer;
begin
  if tg_table_name = 'shifts' then
    if new.status not in ('locked', 'needs_correction') then
      return new;
    end if;
    v_org_id := new.org_id;
    v_station_id := new.station_id;
    v_shift_id := new.shift_id;
    v_report_id := new.current_report_id;
  else
    v_org_id := coalesce(new.org_id, old.org_id);
    v_station_id := coalesce(new.station_id, old.station_id);
    v_shift_id := coalesce(new.shift_id, old.shift_id);
    v_report_id := coalesce(new.report_id, old.report_id);
  end if;
  if v_report_id is null then
    return new;
  end if;
  select s.status into v_status
    from public.shifts s
   where s.org_id = v_org_id and s.station_id = v_station_id and s.shift_id = v_shift_id;
  if v_status not in ('locked', 'needs_correction') then
    return new;
  end if;
  select r.version_no into v_version_no
    from public.shift_reports r
   where r.org_id = v_org_id and r.station_id = v_station_id
     and r.shift_id = v_shift_id and r.report_id = v_report_id;
  select count(*) filter (where active_ack_id is not null)::integer into v_active
    from public.ack_head h
   where h.org_id = v_org_id and h.station_id = v_station_id
     and h.shift_id = v_shift_id and h.report_id = v_report_id
     and h.version_no = v_version_no;
  if v_active <> 1 then
    raise exception using errcode = '23514', message = 'ack_head_cardinality';
  end if;
  return new;
end;
$$;
alter function public.trg_ack_cardinality() owner to report_writer;
create constraint trigger shifts_ack_cardinality after update of status on public.shifts
deferrable initially deferred for each row execute function public.trg_ack_cardinality();
create constraint trigger ack_head_cardinality after insert or update or delete on public.ack_head
deferrable initially deferred for each row execute function public.trg_ack_cardinality();

create or replace function public.trg_ack_supersession_check()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_current uuid;
  v_head uuid;
begin
  select s.current_report_id into v_current
    from public.shifts s
   where s.org_id = new.replacement_org_id and s.station_id = new.replacement_station_id
     and s.shift_id = new.replacement_shift_id;
  if v_current <> new.replacement_report_id then
    raise exception using errcode = '23514', message = 'ack_replacement_not_current';
  end if;
  select active_ack_id into v_head
    from public.ack_head h
   where h.org_id = new.old_org_id and h.station_id = new.old_station_id
     and h.shift_id = new.old_shift_id and h.report_id = new.old_report_id
     and h.version_no = new.old_version_no;
  if v_head is not null then
    raise exception using errcode = '23514', message = 'ack_old_head_not_superseded';
  end if;
  return new;
end;
$$;
alter function public.trg_ack_supersession_check() owner to report_writer;
create constraint trigger ack_supersession_check after insert on public.ack_supersessions
deferrable initially deferred for each row execute function public.trg_ack_supersession_check();

create or replace function public.fn_ack_shift(
  p_shift_id uuid,
  p_report_id uuid,
  p_version_no integer,
  p_decision public.ack_decision,
  p_rejection_reason text default null,
  p_is_break_glass boolean default false,
  p_break_glass_reason text default null
)
returns table (ack_id uuid, report_id uuid, version_no integer, decision public.ack_decision)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_station_id uuid;
  v_status text;
  v_current_report uuid;
  v_submitted_by uuid;
  v_ack_head public.ack_head%rowtype;
  v_seq bigint;
  v_ack_id uuid := app.gen_random_uuid();
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Station Admin', 'Owner', 'Superadmin') then
    raise exception using errcode = '42501', message = 'ack_role_required';
  end if;
  if p_decision is null or p_shift_id is null or p_report_id is null or p_version_no is null then
    raise exception using errcode = '22023', message = 'invalid_ack_request';
  end if;
  if p_decision = 'rejected' and btrim(coalesce(p_rejection_reason, '')) = '' then
    raise exception using errcode = '23514', message = 'rejection_reason_required';
  end if;
  if p_decision = 'acked' and p_rejection_reason is not null then
    raise exception using errcode = '23514', message = 'unexpected_rejection_reason';
  end if;
  if p_is_break_glass and (v_role not in ('Owner', 'Superadmin')
      or btrim(coalesce(p_break_glass_reason, '')) = '') then
    raise exception using errcode = '23514', message = 'break_glass_reason_required';
  end if;
  if not p_is_break_glass and p_break_glass_reason is not null then
    raise exception using errcode = '23514', message = 'unexpected_break_glass_reason';
  end if;

  select s.station_id, s.status, s.current_report_id
    into v_station_id, v_status, v_current_report
    from public.shifts s
   where s.org_id = v_org_id and s.shift_id = p_shift_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations where org_id = v_org_id and station_id = v_station_id for update;
  select s.status, s.current_report_id into v_status, v_current_report
    from public.shifts s
   where s.org_id = v_org_id and s.station_id = v_station_id and s.shift_id = p_shift_id
   for update;
  if v_current_report <> p_report_id or v_status <> 'awaiting_confirmation' then
    raise exception using errcode = '23505', message = 'ack_report_not_pending';
  end if;
  select r.submitted_by into v_submitted_by
    from public.shift_reports r
   where r.org_id = v_org_id and r.station_id = v_station_id and r.shift_id = p_shift_id
     and r.report_id = p_report_id and r.version_no = p_version_no
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'report_not_found';
  end if;
  select * into v_ack_head
    from public.ack_head h
   where h.org_id = v_org_id and h.station_id = v_station_id and h.shift_id = p_shift_id
     and h.report_id = p_report_id and h.version_no = p_version_no
   for update;
  if not found then
    raise exception using errcode = '23514', message = 'ack_head_missing';
  end if;
  if v_ack_head.active_ack_id is not null then
    raise exception using errcode = '23505', message = 'ack_already_decided';
  end if;
  if not p_is_break_glass and (
    v_submitted_by = v_actor
    or exists (select 1 from public.sales_declared x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = p_report_id and x.created_by = v_actor)
    or exists (select 1 from public.loss_entries x where x.org_id = v_org_id and x.station_id = v_station_id and x.report_id = p_report_id and x.created_by = v_actor)
  ) then
    raise exception using errcode = '42501', message = 'ack_separation_required';
  end if;
  select coalesce(max(d.ack_seq), 0) + 1 into v_seq
    from public.ack_decisions d
   where d.org_id = v_org_id and d.station_id = v_station_id and d.shift_id = p_shift_id
     and d.report_id = p_report_id and d.version_no = p_version_no;
  perform set_config('app.transition', 'ack_shift', true);
  insert into public.ack_decisions(
    ack_id, org_id, station_id, shift_id, report_id, version_no, ack_seq, decision,
    actor_user_id, rejection_reason, is_superadmin, is_break_glass, break_glass_reason
  ) values (
    v_ack_id, v_org_id, v_station_id, p_shift_id, p_report_id, p_version_no, v_seq,
    p_decision, v_actor, p_rejection_reason, v_role = 'Superadmin', p_is_break_glass,
    p_break_glass_reason
  );
  update public.ack_head h set active_ack_id = v_ack_id
   where h.org_id = v_org_id and h.station_id = v_station_id and h.shift_id = p_shift_id
     and h.report_id = p_report_id and h.version_no = p_version_no;
  update public.shifts set status = (case when p_decision = 'acked' then 'locked' else 'needs_correction' end)::public.shift_status,
       closed_at = case when p_decision = 'acked' then clock_timestamp() else closed_at end
   where org_id = v_org_id and station_id = v_station_id and shift_id = p_shift_id;
  set constraints all immediate;
  ack_id := v_ack_id;
  report_id := p_report_id;
  version_no := p_version_no;
  decision := p_decision;
  return next;
end;
$$;
alter function public.fn_ack_shift(uuid, uuid, integer, public.ack_decision, text, boolean, text) owner to report_writer;

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

do $$
declare v_table text;
begin
  foreach v_table in array array['ack_decisions', 'ack_head', 'ack_supersessions'] loop
    execute format('alter table public.%I enable row level security', v_table);
    execute format('alter table public.%I force row level security', v_table);
  end loop;
end
$$;
create policy ack_decisions_context on public.ack_decisions
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy ack_head_context on public.ack_head
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy ack_supersessions_context on public.ack_supersessions
using (old_org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or old_station_id::text = current_setting('app.station_id', true)))
with check (old_org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or old_station_id::text = current_setting('app.station_id', true)));

revoke all on table public.ack_decisions, public.ack_head, public.ack_supersessions
  from public, pomkita_app, report_writer, audit_owner, relay;
grant select, insert on public.ack_decisions to report_writer;
grant references on public.ack_decisions to report_writer;
grant select on public.ack_decisions to pomkita_app;
grant select on public.ack_decisions to pomkita;
grant references on public.ack_decisions to pomkita_app, pomkita;
grant update on public.ack_decisions to report_writer, pomkita_app, pomkita;
grant select, insert, update on public.ack_head to report_writer;
grant select, insert on public.ack_supersessions to report_writer;
grant execute on function public.fn_ack_shift(uuid, uuid, integer, public.ack_decision, text, boolean, text) to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_ack_shift', array['Station Admin', 'Owner', 'Superadmin'], 'ack_shift', 53),
  ('trg_ack_guard', array[]::text[], 'internal_trigger', 52),
  ('trg_ack_report_head', array[]::text[], 'internal_trigger', 50),
  ('trg_ack_cardinality', array[]::text[], 'internal_trigger', 52),
  ('trg_ack_supersession_check', array[]::text[], 'internal_trigger', 54)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
