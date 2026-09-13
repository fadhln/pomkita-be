-- PLAN.md section 4.4; BE-PLAN.md Phase B5.

create or replace function public.trg_policy_revision_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op = 'DELETE' then
    raise exception using errcode = '23514', message = 'policy_revision_immutable';
  end if;
  if current_setting('app.transition', true) <> 'policy_revision_tombstone'
     or old.disabled
     or not new.disabled
     or new.rev_id is distinct from old.rev_id
     or new.policy_id is distinct from old.policy_id
     or new.org_id is distinct from old.org_id
     or new.station_id is distinct from old.station_id
     or new.valid_from is distinct from old.valid_from
     or new.supersedes_org_id is distinct from old.supersedes_org_id
     or new.supersedes_rev_id is distinct from old.supersedes_rev_id
     or new.created_by is distinct from old.created_by
     or new.created_at is distinct from old.created_at then
    raise exception using errcode = '23514', message = 'policy_revision_immutable';
  end if;
  return new;
end;
$$;
alter function public.trg_policy_revision_guard() owner to audit_owner;

create or replace function public.trg_evidence_policy_type_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  raise exception using errcode = '23514', message = 'evidence_policy_type_immutable';
end;
$$;
alter function public.trg_evidence_policy_type_guard() owner to audit_owner;

drop trigger if exists threshold_policy_revisions_guard on public.threshold_policy_revisions;
create trigger threshold_policy_revisions_guard
before update or delete on public.threshold_policy_revisions
for each row execute function public.trg_policy_revision_guard();

drop trigger if exists evidence_policy_revisions_guard on public.evidence_policy_revisions;
create trigger evidence_policy_revisions_guard
before update or delete on public.evidence_policy_revisions
for each row execute function public.trg_policy_revision_guard();

drop trigger if exists evidence_policy_types_guard on public.evidence_policy_types;
create trigger evidence_policy_types_guard
before update or delete on public.evidence_policy_types
for each row execute function public.trg_evidence_policy_type_guard();

create or replace function public.fn_create_policy_revision(
  p_policy_kind text,
  p_policy_id uuid,
  p_station_id uuid,
  p_valid_from timestamptz(6),
  p_supersedes_org_id uuid,
  p_supersedes_rev_id uuid,
  p_loss_liter_threshold numeric,
  p_gain_liter_threshold numeric,
  p_loss_rupiah_threshold numeric,
  p_gain_rupiah_threshold numeric,
  p_variance_rupiah_threshold numeric,
  p_rollover_threshold numeric,
  p_mode text,
  p_types jsonb
)
returns table (
  revision_id uuid,
  policy_kind text,
  policy_id uuid,
  station_id uuid,
  valid_from timestamptz(6),
  disabled boolean
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_revision uuid := app.gen_random_uuid();
  v_item jsonb;
  v_type text;
  v_minimum integer;
  v_mime text;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null or v_actor is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Owner', 'Superadmin')
     or not exists (
       select 1 from public.user_station_roles r
        where r.org_id = v_org_id and r.user_id = v_actor
          and r.role in ('Owner', 'Superadmin')) then
    raise exception using errcode = '42501', message = 'policy_revision_role_required';
  end if;
  if p_policy_kind not in ('threshold', 'evidence') or p_policy_id is null
     or p_valid_from is null or (p_supersedes_org_id is null) <> (p_supersedes_rev_id is null)
     or (p_station_id is not null and nullif(current_setting('app.station_id', true), '') is not null
         and p_station_id <> nullif(current_setting('app.station_id', true), '')::uuid) then
    raise exception using errcode = '22023', message = 'invalid_policy_revision_request';
  end if;
  if p_supersedes_org_id is not null and p_supersedes_org_id <> v_org_id then
    raise exception using errcode = '42501', message = 'policy_supersede_target_not_found';
  end if;
  if exists (
    select 1
      from public.threshold_policy_revisions p
     where p.org_id = v_org_id and p.station_id is not distinct from p_station_id
       and p.valid_from = p_valid_from) and p_policy_kind = 'threshold' then
    raise exception using errcode = '23505', message = 'policy_revision_overlap';
  end if;
  if exists (
    select 1
      from public.evidence_policy_revisions p
     where p.org_id = v_org_id and p.station_id is not distinct from p_station_id
       and p.valid_from = p_valid_from) and p_policy_kind = 'evidence' then
    raise exception using errcode = '23505', message = 'policy_revision_overlap';
  end if;

  if p_policy_kind = 'threshold' then
    if p_loss_liter_threshold is null or p_gain_liter_threshold is null
       or p_loss_rupiah_threshold is null or p_gain_rupiah_threshold is null
       or p_variance_rupiah_threshold is null
       or p_loss_liter_threshold < 0 or p_gain_liter_threshold < 0
       or p_loss_rupiah_threshold < 0 or p_gain_rupiah_threshold < 0
       or p_variance_rupiah_threshold < 0
       or (p_rollover_threshold is not null and p_rollover_threshold < 0)
       or p_mode is not null or p_types is not null then
      raise exception using errcode = '23514', message = 'invalid_threshold_policy';
    end if;
  else
    if p_mode not in ('opsional', 'wajib') or jsonb_typeof(p_types) <> 'array'
       or jsonb_array_length(p_types) = 0
       or p_loss_liter_threshold is not null or p_gain_liter_threshold is not null
       or p_loss_rupiah_threshold is not null or p_gain_rupiah_threshold is not null
       or p_variance_rupiah_threshold is not null or p_rollover_threshold is not null then
      raise exception using errcode = '23514', message = 'invalid_evidence_policy';
    end if;
    if jsonb_array_length(p_types) <> (
      select count(distinct x.item->>'evidence_type') from jsonb_array_elements(p_types) x(item)
    ) then
      raise exception using errcode = '23514', message = 'duplicate_evidence_type';
    end if;
    for v_item in select value from jsonb_array_elements(p_types) loop
      v_type := btrim(coalesce(v_item->>'evidence_type', ''));
      if jsonb_typeof(v_item) <> 'object' or v_type = ''
         or jsonb_typeof(v_item->'minimum_count_per_loss') <> 'number'
         or coalesce(v_item->>'minimum_count_per_loss', '') !~ '^[0-9]+$'
         or (v_item->>'minimum_count_per_loss')::numeric > 2147483647
         or jsonb_typeof(v_item->'accepted_mime_types') <> 'array'
         or jsonb_array_length(v_item->'accepted_mime_types') = 0 then
        raise exception using errcode = '23514', message = 'invalid_evidence_policy_type';
      end if;
      v_minimum := (v_item->>'minimum_count_per_loss')::integer;
      if p_mode = 'wajib' and v_minimum < 1 then
        raise exception using errcode = '23514', message = 'required_evidence_minimum_missing';
      end if;
      for v_mime in select jsonb_array_elements_text(v_item->'accepted_mime_types') loop
        if v_mime !~ '^[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*$' then
          raise exception using errcode = '23514', message = 'invalid_evidence_mime_type';
        end if;
      end loop;
    end loop;
  end if;

  if p_supersedes_rev_id is not null then
    if p_policy_kind = 'threshold' then
      if not exists (
        select 1 from public.threshold_policy_revisions p
         where p.org_id = v_org_id and p.rev_id = p_supersedes_rev_id
           and p.policy_id = p_policy_id and p.station_id is not distinct from p_station_id
         for update) then
        raise exception using errcode = '42501', message = 'policy_supersede_target_not_found';
      end if;
      if exists (select 1 from public.threshold_policy_revisions p where p.org_id = v_org_id and p.supersedes_rev_id = p_supersedes_rev_id) then
        raise exception using errcode = '23505', message = 'policy_already_superseded';
      end if;
      if p_valid_from <= (select p.valid_from from public.threshold_policy_revisions p where p.org_id = v_org_id and p.rev_id = p_supersedes_rev_id) then
        raise exception using errcode = '23505', message = 'policy_valid_from_order';
      end if;
    else
      if not exists (
        select 1 from public.evidence_policy_revisions p
         where p.org_id = v_org_id and p.rev_id = p_supersedes_rev_id
           and p.policy_id = p_policy_id and p.station_id is not distinct from p_station_id
         for update) then
        raise exception using errcode = '42501', message = 'policy_supersede_target_not_found';
      end if;
      if exists (select 1 from public.evidence_policy_revisions p where p.org_id = v_org_id and p.supersedes_rev_id = p_supersedes_rev_id) then
        raise exception using errcode = '23505', message = 'policy_already_superseded';
      end if;
      if p_valid_from <= (select p.valid_from from public.evidence_policy_revisions p where p.org_id = v_org_id and p.rev_id = p_supersedes_rev_id) then
        raise exception using errcode = '23505', message = 'policy_valid_from_order';
      end if;
    end if;
  end if;

  if p_policy_kind = 'threshold' then
    insert into public.threshold_policy_revisions(
      rev_id, policy_id, org_id, station_id, valid_from,
      supersedes_org_id, supersedes_rev_id, loss_liter_threshold,
      gain_liter_threshold, loss_rupiah_threshold, gain_rupiah_threshold,
      variance_rupiah_threshold, rollover_threshold, created_by)
    values (
      v_revision, p_policy_id, v_org_id, p_station_id, p_valid_from,
      p_supersedes_org_id, p_supersedes_rev_id, p_loss_liter_threshold,
      p_gain_liter_threshold, p_loss_rupiah_threshold, p_gain_rupiah_threshold,
      p_variance_rupiah_threshold, p_rollover_threshold, v_actor);
  else
    insert into public.evidence_policy_revisions(
      rev_id, policy_id, org_id, station_id, valid_from,
      supersedes_org_id, supersedes_rev_id, mode, created_by)
    values (
      v_revision, p_policy_id, v_org_id, p_station_id, p_valid_from,
      p_supersedes_org_id, p_supersedes_rev_id, p_mode, v_actor);
    insert into public.evidence_policy_types(org_id, rev_id, evidence_type, minimum_count_per_loss, accepted_mime_types)
    select v_org_id, v_revision, x->>'evidence_type',
           (x->>'minimum_count_per_loss')::integer,
           array(select jsonb_array_elements_text(x->'accepted_mime_types'))
      from jsonb_array_elements(p_types) x;
  end if;

  perform public.fn_append_audit_event(
    app.gen_random_uuid(), 'policy_revision_created',
    jsonb_build_object('revision_id', v_revision, 'policy_kind', p_policy_kind,
      'policy_id', p_policy_id, 'station_id', p_station_id, 'valid_from', p_valid_from,
      'scope', case when p_station_id is null then 'organization' else 'station' end),
    'success', null);
  revision_id := v_revision;
  policy_kind := p_policy_kind;
  policy_id := p_policy_id;
  station_id := p_station_id;
  valid_from := p_valid_from;
  disabled := false;
  return next;
end;
$$;
alter function public.fn_create_policy_revision(text,uuid,uuid,timestamptz,uuid,uuid,numeric,numeric,numeric,numeric,numeric,numeric,text,jsonb) owner to audit_owner;

create or replace function public.fn_tombstone_policy_revision(
  p_policy_kind text,
  p_revision_id uuid,
  p_reason text
)
returns table (
  revision_id uuid,
  policy_kind text,
  policy_id uuid,
  station_id uuid,
  valid_from timestamptz(6),
  disabled boolean
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_role text := current_setting('app.role', true);
  v_policy_id uuid;
  v_station_id uuid;
  v_valid_from timestamptz(6);
  v_disabled boolean;
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null or v_actor is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  if v_role not in ('Owner', 'Superadmin')
     or not exists (select 1 from public.user_station_roles r where r.org_id = v_org_id and r.user_id = v_actor and r.role in ('Owner', 'Superadmin')) then
    raise exception using errcode = '42501', message = 'policy_revision_role_required';
  end if;
  if p_policy_kind not in ('threshold', 'evidence') or p_revision_id is null or btrim(coalesce(p_reason, '')) = '' then
    raise exception using errcode = '23514', message = 'tombstone_reason_required';
  end if;

  if p_policy_kind = 'threshold' then
    select p.policy_id, p.station_id, p.valid_from, p.disabled
      into v_policy_id, v_station_id, v_valid_from, v_disabled
      from public.threshold_policy_revisions p
     where p.org_id = v_org_id and p.rev_id = p_revision_id
       and (nullif(current_setting('app.station_id', true), '') is null or p.station_id is null or p.station_id = nullif(current_setting('app.station_id', true), '')::uuid)
     for update;
  else
    select p.policy_id, p.station_id, p.valid_from, p.disabled
      into v_policy_id, v_station_id, v_valid_from, v_disabled
      from public.evidence_policy_revisions p
     where p.org_id = v_org_id and p.rev_id = p_revision_id
       and (nullif(current_setting('app.station_id', true), '') is null or p.station_id is null or p.station_id = nullif(current_setting('app.station_id', true), '')::uuid)
     for update;
  end if;
  if not found then
    raise exception using errcode = '42501', message = 'policy_revision_not_found';
  end if;
  if v_disabled then
    raise exception using errcode = '23505', message = 'policy_revision_already_disabled';
  end if;

  perform set_config('app.transition', 'policy_revision_tombstone', true);
  if p_policy_kind = 'threshold' then
    update public.threshold_policy_revisions set disabled = true where org_id = v_org_id and rev_id = p_revision_id;
  else
    update public.evidence_policy_revisions set disabled = true where org_id = v_org_id and rev_id = p_revision_id;
  end if;
  perform public.fn_append_audit_event(
    app.gen_random_uuid(), 'policy_revision_tombstoned',
    jsonb_build_object('revision_id', p_revision_id, 'policy_kind', p_policy_kind,
      'reason', btrim(p_reason), 'valid_from', v_valid_from), 'success', null);
  revision_id := p_revision_id;
  policy_kind := p_policy_kind;
  policy_id := v_policy_id;
  station_id := v_station_id;
  valid_from := v_valid_from;
  disabled := true;
  return next;
end;
$$;
alter function public.fn_tombstone_policy_revision(text,uuid,text) owner to audit_owner;

grant select on public.user_station_roles to audit_owner;
grant insert, select, update on public.threshold_policy_revisions, public.evidence_policy_revisions to audit_owner;
grant insert, select on public.evidence_policy_types to audit_owner;
grant execute on function public.fn_append_audit_event(uuid,text,jsonb,text,text) to audit_owner;
grant execute on function public.fn_create_policy_revision(text,uuid,uuid,timestamptz,uuid,uuid,numeric,numeric,numeric,numeric,numeric,numeric,text,jsonb), public.fn_tombstone_policy_revision(text,uuid,text) to pomkita_app;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values
  ('fn_create_policy_revision', array['Owner', 'Superadmin'], 'create_policy_revision', 47),
  ('fn_tombstone_policy_revision', array['Owner', 'Superadmin'], 'tombstone_policy_revision', 47),
  ('trg_policy_revision_guard', array[]::text[], 'internal_trigger', 47),
  ('trg_evidence_policy_type_guard', array[]::text[], 'internal_trigger', 47)
on conflict (name) do update set allowed_roles = excluded.allowed_roles,
  action = excluded.action, lock_rank = excluded.lock_rank;
