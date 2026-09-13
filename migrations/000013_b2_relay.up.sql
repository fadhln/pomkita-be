-- PLAN.md sections 7, 8, and 9; BE-PLAN.md Phase B2.

create table public.outbox_relay_state (
  org_id uuid not null,
  event_id uuid not null,
  relay_status text not null default 'pending',
  attempt_count integer not null default 0,
  lease_token uuid,
  lease_expires_at timestamptz(6),
  last_attempt_at timestamptz(6),
  next_attempt_at timestamptz(6),
  delivered_at timestamptz(6),
  last_error text,
  primary key (org_id, event_id),
  foreign key (org_id, event_id) references public.audit_outbox(org_id, event_id),
  check (relay_status in ('pending', 'in_flight', 'failed', 'delivered')),
  check (attempt_count >= 0),
  check ((relay_status = 'in_flight' and lease_token is not null and lease_expires_at is not null)
      or relay_status <> 'in_flight'),
  check ((relay_status = 'delivered' and delivered_at is not null) or relay_status <> 'delivered')
);
alter table public.outbox_relay_state owner to audit_owner;

create or replace function public.trg_relay_state_init()
returns trigger language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
begin
  insert into public.outbox_relay_state(org_id, event_id, next_attempt_at)
  values (new.org_id, new.event_id, new.created_at)
  on conflict (org_id, event_id) do nothing;
  return new;
end;
$$;
alter function public.trg_relay_state_init() owner to audit_owner;
create trigger audit_outbox_relay_state after insert on public.audit_outbox
for each row execute function public.trg_relay_state_init();

create or replace function public.fn_relay_claim_event(p_org_id uuid, p_event_id uuid)
returns table (event_id uuid, event_type text, payload jsonb, created_at timestamptz(6), lease_token uuid)
language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_now timestamptz(6) := clock_timestamp();
  v_token uuid := app.gen_random_uuid();
  v_status text;
  v_next_attempt timestamptz(6);
begin
  if p_org_id is null or p_event_id is null then
    raise exception using errcode = '22023', message = 'invalid_relay_claim';
  end if;
  select s.relay_status, s.next_attempt_at into v_status, v_next_attempt
    from public.outbox_relay_state s
   where s.org_id = p_org_id and s.event_id = p_event_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'relay_event_not_found';
  end if;
  if v_status = 'delivered' or (v_status = 'in_flight' and (select lease_expires_at from public.outbox_relay_state where org_id = p_org_id and event_id = p_event_id) > v_now)
     or (v_status = 'failed' and v_next_attempt is not null and v_next_attempt > v_now) then
    return;
  end if;
  update public.outbox_relay_state s set relay_status = 'in_flight', attempt_count = s.attempt_count + 1,
    lease_token = v_token, lease_expires_at = v_now + interval '5 minutes', last_attempt_at = v_now,
    last_error = null
   where s.org_id = p_org_id and s.event_id = p_event_id;
  return query select o.event_id, o.event_type, o.payload, o.created_at, v_token
    from public.audit_outbox o where o.org_id = p_org_id and o.event_id = p_event_id;
end;
$$;
alter function public.fn_relay_claim_event(uuid, uuid) owner to audit_owner;

create or replace function public.fn_relay_finish_event(
  p_org_id uuid, p_event_id uuid, p_lease_token uuid, p_success boolean, p_error text default null
)
returns boolean language plpgsql security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_attempt integer;
  v_now timestamptz(6) := clock_timestamp();
  v_delay integer;
begin
  select attempt_count into v_attempt from public.outbox_relay_state
   where org_id = p_org_id and event_id = p_event_id and lease_token = p_lease_token
     and relay_status = 'in_flight' and lease_expires_at > v_now for update;
  if not found then
    raise exception using errcode = '23505', message = 'relay_lease_conflict';
  end if;
  if p_success then
    update public.outbox_relay_state set relay_status = 'delivered', lease_token = null,
      lease_expires_at = null, delivered_at = v_now, next_attempt_at = null, last_error = null
     where org_id = p_org_id and event_id = p_event_id;
  else
    v_delay := least(3600, (2 ^ least(v_attempt, 12)));
    update public.outbox_relay_state set relay_status = 'failed', lease_token = null,
      lease_expires_at = null, next_attempt_at = v_now + make_interval(secs => v_delay),
      last_error = left(coalesce(p_error, 'relay_failed'), 1000)
     where org_id = p_org_id and event_id = p_event_id;
  end if;
  return true;
end;
$$;
alter function public.fn_relay_finish_event(uuid, uuid, uuid, boolean, text) owner to audit_owner;

alter table public.outbox_relay_state enable row level security;
alter table public.outbox_relay_state force row level security;
create policy outbox_relay_state_context on public.outbox_relay_state
using (org_id::text = coalesce(nullif(current_setting('app.relay_org_id', true), ''), current_setting('app.org_id', true)))
with check (org_id::text = coalesce(nullif(current_setting('app.relay_org_id', true), ''), current_setting('app.org_id', true)));

revoke all on table public.outbox_relay_state from public, pomkita_app, report_writer, audit_owner, relay;
grant select, update on public.outbox_relay_state to audit_owner;
grant select on public.audit_outbox to audit_owner;
grant execute on function public.fn_relay_claim_event(uuid, uuid) to relay;
grant execute on function public.fn_relay_finish_event(uuid, uuid, uuid, boolean, text) to relay;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_relay_claim_event', array[]::text[], 'relay_claim_event', 110),
  ('fn_relay_finish_event', array[]::text[], 'relay_finish_event', 110),
  ('trg_relay_state_init', array[]::text[], 'internal_trigger', 110)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
