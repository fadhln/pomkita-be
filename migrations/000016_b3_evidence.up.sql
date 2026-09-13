-- PLAN.md sections 4.2, 4.4, 6, 7, and 8; BE-PLAN.md Phase B3.

create table public.evidence_event (
  evidence_event_id uuid primary key default app.gen_random_uuid(),
  evidence_id uuid not null,
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  report_id uuid not null,
  loss_row_id uuid not null,
  event_seq integer not null,
  event_type text not null,
  evidence_type text not null,
  object_key text not null,
  content_hash bytea not null,
  size_bytes bigint not null,
  mime text not null,
  actor_user_id uuid not null,
  at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, shift_id, report_id, loss_row_id, evidence_id, event_seq),
  foreign key (org_id, station_id, shift_id, report_id)
    references public.shift_reports(org_id, station_id, shift_id, report_id),
  foreign key (org_id, station_id, shift_id, report_id, loss_row_id)
    references public.loss_entries(org_id, station_id, shift_id, report_id, row_id),
  foreign key (org_id, actor_user_id) references public.users(org_id, user_id),
  check (event_seq >= 1),
  check (event_type in ('uploaded', 'verified', 'finalized')),
  check (btrim(evidence_type) <> ''),
  check (btrim(object_key) <> ''),
  check (octet_length(content_hash) = 32),
  check (size_bytes > 0),
  check (btrim(mime) <> '')
);
alter table public.evidence_event owner to report_writer;

create table public.loss_exception (
  exception_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  shift_id uuid not null,
  report_id uuid not null,
  loss_row_id uuid not null,
  reason text not null,
  actor_user_id uuid not null,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, shift_id, report_id, loss_row_id),
  foreign key (org_id, station_id, shift_id, report_id)
    references public.shift_reports(org_id, station_id, shift_id, report_id),
  foreign key (org_id, station_id, shift_id, report_id, loss_row_id)
    references public.loss_entries(org_id, station_id, shift_id, report_id, row_id),
  foreign key (org_id, actor_user_id) references public.users(org_id, user_id),
  check (btrim(reason) <> '')
);
alter table public.loss_exception owner to report_writer;

create or replace function public.fn_validate_evidence(p_report_id uuid)
returns void
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_shift_id uuid;
  v_payload jsonb;
  v_mode text;
  v_loss record;
  v_event record;
  v_type jsonb;
  v_final_count integer;
  v_has_final boolean;
  v_exception_count integer;
  v_type_allowed boolean;
  v_mime_allowed boolean;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select r.station_id, r.shift_id, i.payload
    into v_station_id, v_shift_id, v_payload
    from public.shift_reports r
    join public.policy_snapshot_items i
      on i.org_id = r.org_id and i.station_id = r.station_id
     and i.shift_id = r.shift_id and i.set_id = r.policy_snapshot_set_id
     and i.policy_kind = 'evidence'
   where r.org_id = v_org_id and r.report_id = p_report_id;
  if not found then
    raise exception using errcode = '42501', message = 'report_not_found';
  end if;
  v_mode := v_payload->>'mode';
  if v_mode not in ('wajib', 'opsional') or jsonb_typeof(v_payload->'types') <> 'array' then
    raise exception using errcode = '23514', message = 'evidence_snapshot_invalid';
  end if;

  for v_loss in
    select l.row_id, l.loss_id
      from public.loss_entries l
     where l.org_id = v_org_id and l.station_id = v_station_id
       and l.shift_id = v_shift_id and l.report_id = p_report_id
     order by l.row_id
  loop
    for v_event in
      select e.evidence_id, e.evidence_type, e.mime, e.event_type
        from public.evidence_event e
       where e.org_id = v_org_id and e.station_id = v_station_id
         and e.shift_id = v_shift_id and e.report_id = p_report_id
         and e.loss_row_id = v_loss.row_id
       order by e.evidence_id, e.event_seq
    loop
      select exists (
        select 1 from jsonb_array_elements(v_payload->'types') x
         where x->>'type' = v_event.evidence_type) into v_type_allowed;
      if not v_type_allowed then
        raise exception using errcode = '23514', message = 'evidence_type_not_allowed';
      end if;
      select exists (
        select 1 from jsonb_array_elements(v_payload->'types') x,
             jsonb_array_elements_text(x->'accepted_mime_types') m
         where x->>'type' = v_event.evidence_type and m = v_event.mime) into v_mime_allowed;
      if not v_mime_allowed then
        raise exception using errcode = '23514', message = 'evidence_mime_not_allowed';
      end if;
    end loop;

    select exists (
      select 1 from public.evidence_event e
       where e.org_id = v_org_id and e.station_id = v_station_id
         and e.shift_id = v_shift_id and e.report_id = p_report_id
         and e.loss_row_id = v_loss.row_id and e.event_type = 'finalized') into v_has_final;
    select count(*) into v_exception_count
      from public.loss_exception x
     where x.org_id = v_org_id and x.station_id = v_station_id
       and x.shift_id = v_shift_id and x.report_id = p_report_id
       and x.loss_row_id = v_loss.row_id;

    for v_type in select value from jsonb_array_elements(v_payload->'types') loop
      select count(distinct e.evidence_id) into v_final_count
        from public.evidence_event e
       where e.org_id = v_org_id and e.station_id = v_station_id
         and e.shift_id = v_shift_id and e.report_id = p_report_id
         and e.loss_row_id = v_loss.row_id and e.event_type = 'finalized'
         and e.evidence_type = v_type->>'type';
      if v_final_count < (v_type->>'minimum_count_per_loss')::integer then
        if v_mode = 'wajib' then
          raise exception using errcode = '23514', message = 'evidence_required';
        end if;
        v_has_final := false;
      end if;
    end loop;

    if v_mode = 'wajib' and v_exception_count > 0 then
      raise exception using errcode = '23514', message = 'evidence_exception_forbidden';
    elsif v_mode = 'opsional' and not v_has_final and v_exception_count <> 1 then
      raise exception using errcode = '23514', message = 'evidence_exception_required';
    elsif v_mode = 'opsional' and v_has_final and v_exception_count > 0 then
      raise exception using errcode = '23514', message = 'evidence_exception_with_evidence';
    end if;
  end loop;
end;
$$;
alter function public.fn_validate_evidence(uuid) owner to report_writer;

create or replace function public.trg_evidence_event_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_previous text;
  v_previous_seq integer;
begin
  if tg_op <> 'INSERT' or current_setting('app.transition', true) not in ('submit_shift', 'approve_amendment', 'evidence_event') then
    raise exception using errcode = '23514', message = 'evidence_event_write_forbidden';
  end if;
  select e.event_type, e.event_seq into v_previous, v_previous_seq
    from public.evidence_event e
   where e.org_id = new.org_id and e.station_id = new.station_id
     and e.shift_id = new.shift_id and e.report_id = new.report_id
     and e.loss_row_id = new.loss_row_id and e.evidence_id = new.evidence_id
   order by e.event_seq desc limit 1;
  if not found then
    if new.event_seq <> 1 or new.event_type not in ('uploaded', 'finalized') then
      raise exception using errcode = '23514', message = 'evidence_event_sequence_invalid';
    end if;
  elsif new.event_seq <> v_previous_seq + 1
     or (v_previous = 'uploaded' and new.event_type <> 'verified')
     or (v_previous = 'verified' and new.event_type <> 'finalized')
     or v_previous = 'finalized' then
    raise exception using errcode = '23514', message = 'evidence_event_sequence_invalid';
  end if;
  return new;
end;
$$;
alter function public.trg_evidence_event_guard() owner to report_writer;
create trigger evidence_event_guard before insert or update or delete on public.evidence_event
for each row execute function public.trg_evidence_event_guard();

create or replace function public.trg_evidence_safety_net()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_table_name = 'loss_exception' then
    perform public.fn_validate_evidence(new.report_id);
  elsif tg_table_name = 'shift_reports' and new.status = 'submitted' then
    perform public.fn_validate_evidence(new.report_id);
  end if;
  return new;
end;
$$;
alter function public.trg_evidence_safety_net() owner to report_writer;
create constraint trigger loss_exception_evidence_safety after insert on public.loss_exception
deferrable initially deferred for each row execute function public.trg_evidence_safety_net();
create constraint trigger shift_report_evidence_safety after insert or update of status on public.shift_reports
deferrable initially deferred for each row execute function public.trg_evidence_safety_net();

create or replace function public.trg_loss_exception_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  raise exception using errcode = '23514', message = 'loss_exception_immutable';
end;
$$;
alter function public.trg_loss_exception_guard() owner to report_writer;
create trigger loss_exception_guard before update or delete on public.loss_exception
for each row execute function public.trg_loss_exception_guard();

alter table public.evidence_event enable row level security;
alter table public.evidence_event force row level security;
alter table public.loss_exception enable row level security;
alter table public.loss_exception force row level security;
create policy evidence_event_context on public.evidence_event
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));
create policy loss_exception_context on public.loss_exception
using (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)))
with check (org_id::text = current_setting('app.org_id', true)
   and (current_setting('app.station_id', true) = '' or station_id::text = current_setting('app.station_id', true)));

revoke all on table public.evidence_event, public.loss_exception from public, pomkita_app, report_writer, audit_owner, relay;
grant select, insert on public.evidence_event to report_writer;
grant select, insert on public.loss_exception to report_writer;
grant execute on function public.fn_validate_evidence(uuid) to pomkita_app, report_writer;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_validate_evidence', array[]::text[], 'validate_evidence', 51),
  ('trg_evidence_event_guard', array[]::text[], 'internal_trigger', 51),
  ('trg_evidence_safety_net', array[]::text[], 'internal_trigger', 51),
  ('trg_loss_exception_guard', array[]::text[], 'internal_trigger', 51)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
