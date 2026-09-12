-- PLAN.md sections 4.2, 4.3, 5.1, 7, and 8; BE-PLAN.md Phase B1.

alter table public.draft_losses
  add constraint draft_losses_row_scope_key unique (org_id, station_id, draft_id, row_id);
alter table public.draft_evidence_staging
  add constraint draft_evidence_loss_fk
  foreign key (org_id, station_id, draft_id, loss_row_id)
  references public.draft_losses (org_id, station_id, draft_id, row_id);

create or replace function public.fn_claim_draft(p_shift_id uuid)
returns table (draft_id uuid, claim_token uuid, claim_expires_at timestamptz(6), revision integer)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_station_id uuid;
  v_draft_id uuid;
  v_new_token uuid := app.gen_random_uuid();
begin
  if current_setting('app.context_valid', true) <> 'true' or v_org_id is null or v_actor is null then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select s.station_id into v_station_id
    from public.shifts s
   where s.org_id = v_org_id and s.shift_id = p_shift_id;
  if not found then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations where org_id = v_org_id and station_id = v_station_id for update;
  select d.draft_id into v_draft_id
    from public.shift_drafts d
   where d.org_id = v_org_id and d.station_id = v_station_id and d.shift_id = p_shift_id
   for update;
  if not found then
    raise exception using errcode = '42501', message = 'draft_not_found';
  end if;
  if exists (
    select 1 from public.shift_drafts d
     where d.org_id = v_org_id and d.draft_id = v_draft_id
       and d.claim_expires_at > clock_timestamp() and d.owned_by <> v_actor
  ) then
    raise exception using errcode = '23505', message = 'draft_claim_conflict';
  end if;
  update public.shift_drafts d
     set owned_by = v_actor,
         claim_token = v_new_token,
         claim_expires_at = clock_timestamp() + interval '10 minutes',
         revision = d.revision + 1,
         updated_by = v_actor,
         updated_at = clock_timestamp()
   where d.org_id = v_org_id and d.station_id = v_station_id and d.draft_id = v_draft_id
   returning d.claim_token, d.claim_expires_at, d.revision into claim_token, claim_expires_at, revision;
  draft_id := v_draft_id;
  return next;
end;
$$;
alter function public.fn_claim_draft(uuid) owner to station_owner;

create or replace function public.fn_heartbeat_draft(p_draft_id uuid, p_claim_token uuid)
returns boolean
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  update public.shift_drafts
     set claim_expires_at = clock_timestamp() + interval '10 minutes',
         updated_by = v_actor, updated_at = clock_timestamp()
   where org_id = v_org_id and draft_id = p_draft_id and owned_by = v_actor
     and claim_token = p_claim_token and status = 'submitting'
     and claim_expires_at > clock_timestamp();
  return found;
end;
$$;
alter function public.fn_heartbeat_draft(uuid, uuid) owner to station_owner;

create or replace function public.fn_fence_draft(
  p_draft_id uuid, p_claim_token uuid, p_expected_revision integer
)
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_revision integer;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  update public.shift_drafts d
     set revision = d.revision + 1, updated_by = v_actor, updated_at = clock_timestamp()
   where d.org_id = v_org_id and d.draft_id = p_draft_id and d.owned_by = v_actor
     and d.claim_token = p_claim_token and d.claim_expires_at > clock_timestamp()
     and d.revision = p_expected_revision and d.status = 'editing'
   returning d.revision into v_revision;
  if not found then
    raise exception using errcode = '23505', message = 'draft_fence_conflict';
  end if;
  return v_revision;
end;
$$;
alter function public.fn_fence_draft(uuid, uuid, integer) owner to station_owner;

create or replace function public.fn_write_draft_reading(
  p_draft_id uuid, p_claim_token uuid, p_expected_revision integer,
  p_nozzle_id uuid, p_meter_start numeric, p_meter_end numeric
)
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_revision integer;
begin
  select station_id into v_station_id from public.shift_drafts where org_id = v_org_id and draft_id = p_draft_id;
  v_revision := public.fn_fence_draft(p_draft_id, p_claim_token, p_expected_revision);
  insert into public.draft_readings (org_id, station_id, draft_id, nozzle_id, meter_start, meter_end, created_by)
  values (v_org_id, v_station_id, p_draft_id, p_nozzle_id, p_meter_start, p_meter_end, v_actor)
  on conflict (org_id, station_id, draft_id, nozzle_id) do update
    set meter_start = excluded.meter_start, meter_end = excluded.meter_end, created_by = excluded.created_by;
  return v_revision;
end;
$$;
alter function public.fn_write_draft_reading(uuid, uuid, integer, uuid, numeric, numeric) owner to station_owner;

create or replace function public.fn_write_draft_sales(
  p_draft_id uuid, p_claim_token uuid, p_expected_revision integer,
  p_dispenser_id uuid, p_cash_amount numeric, p_cashless_amount numeric
)
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_revision integer;
begin
  select station_id into v_station_id from public.shift_drafts where org_id = v_org_id and draft_id = p_draft_id;
  v_revision := public.fn_fence_draft(p_draft_id, p_claim_token, p_expected_revision);
  insert into public.draft_sales (org_id, station_id, draft_id, dispenser_id, cash_amount, cashless_amount, created_by)
  values (v_org_id, v_station_id, p_draft_id, p_dispenser_id, p_cash_amount, p_cashless_amount, v_actor)
  on conflict (org_id, station_id, draft_id, dispenser_id) do update
    set cash_amount = excluded.cash_amount, cashless_amount = excluded.cashless_amount, created_by = excluded.created_by;
  return v_revision;
end;
$$;
alter function public.fn_write_draft_sales(uuid, uuid, integer, uuid, numeric, numeric) owner to station_owner;

create or replace function public.fn_write_draft_loss(
  p_draft_id uuid, p_claim_token uuid, p_expected_revision integer,
  p_loss_id uuid, p_direction text, p_reason_code text, p_liters numeric,
  p_cash_amount numeric, p_note text
)
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_revision integer;
begin
  select station_id into v_station_id from public.shift_drafts where org_id = v_org_id and draft_id = p_draft_id;
  v_revision := public.fn_fence_draft(p_draft_id, p_claim_token, p_expected_revision);
  insert into public.draft_losses (org_id, station_id, draft_id, loss_id, direction, reason_code, liters, cash_amount, note, created_by)
  values (v_org_id, v_station_id, p_draft_id, p_loss_id, p_direction, p_reason_code, p_liters, p_cash_amount, p_note, v_actor)
  on conflict (org_id, station_id, draft_id, loss_id) do update
    set direction = excluded.direction, reason_code = excluded.reason_code, liters = excluded.liters,
        cash_amount = excluded.cash_amount, note = excluded.note;
  return v_revision;
end;
$$;
alter function public.fn_write_draft_loss(uuid, uuid, integer, uuid, text, text, numeric, numeric, text) owner to station_owner;

create or replace function public.fn_stage_draft_evidence(
  p_draft_id uuid, p_claim_token uuid, p_expected_revision integer,
  p_loss_row_id uuid, p_evidence_type text, p_object_key text,
  p_content_hash bytea, p_size_bytes bigint, p_mime text
)
returns integer
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_station_id uuid;
  v_actor uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  v_revision integer;
begin
  select station_id into v_station_id from public.shift_drafts where org_id = v_org_id and draft_id = p_draft_id;
  v_revision := public.fn_fence_draft(p_draft_id, p_claim_token, p_expected_revision);
  insert into public.draft_evidence_staging
    (org_id, station_id, draft_id, loss_row_id, evidence_type, object_key, content_hash, size_bytes, mime, uploaded_by)
  values (v_org_id, v_station_id, p_draft_id, p_loss_row_id, p_evidence_type, p_object_key, p_content_hash, p_size_bytes, p_mime, v_actor);
  return v_revision;
end;
$$;
alter function public.fn_stage_draft_evidence(uuid, uuid, integer, uuid, text, text, bytea, bigint, text) owner to station_owner;

create or replace function public.read_draft(p_shift_id uuid)
returns jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_org_id uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  v_result jsonb;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select jsonb_build_object(
    'draft_id', d.draft_id::text, 'shift_id', d.shift_id::text, 'status', d.status::text,
    'claim_expires_at', d.claim_expires_at, 'revision', d.revision,
    'readings', coalesce((select jsonb_agg(jsonb_build_object(
      'row_id', r.row_id::text, 'nozzle_id', r.nozzle_id::text,
      'meter_start', r.meter_start::text, 'meter_end', r.meter_end::text
    ) order by r.nozzle_id) from public.draft_readings r where r.org_id = d.org_id and r.station_id = d.station_id and r.draft_id = d.draft_id), '[]'::jsonb),
    'sales', coalesce((select jsonb_agg(jsonb_build_object(
      'row_id', s.row_id::text, 'dispenser_id', s.dispenser_id::text,
      'cash_amount', s.cash_amount::text, 'cashless_amount', s.cashless_amount::text
    ) order by s.dispenser_id) from public.draft_sales s where s.org_id = d.org_id and s.station_id = d.station_id and s.draft_id = d.draft_id), '[]'::jsonb),
    'losses', coalesce((select jsonb_agg(jsonb_build_object(
      'row_id', l.row_id::text, 'loss_id', l.loss_id::text, 'direction', l.direction,
      'reason_code', l.reason_code, 'liters', l.liters::text, 'cash_amount', l.cash_amount::text, 'note', l.note
    ) order by l.row_id) from public.draft_losses l where l.org_id = d.org_id and l.station_id = d.station_id and l.draft_id = d.draft_id), '[]'::jsonb)
  ) into v_result
  from public.shift_drafts d
  where d.org_id = v_org_id and d.shift_id = p_shift_id
    and (current_setting('app.station_id', true) = '' or d.station_id::text = current_setting('app.station_id', true));
  if v_result is null then
    raise exception using errcode = '42501', message = 'draft_not_found';
  end if;
  return v_result;
end;
$$;
alter function public.read_draft(uuid) owner to station_owner;

revoke all on all functions in schema public from public, pomkita_app, report_writer, audit_owner, relay;
grant execute on function public.fn_claim_draft(uuid) to pomkita_app;
grant execute on function public.fn_heartbeat_draft(uuid, uuid) to pomkita_app;
grant execute on function public.fn_fence_draft(uuid, uuid, integer) to pomkita_app;
grant execute on function public.fn_write_draft_reading(uuid, uuid, integer, uuid, numeric, numeric) to pomkita_app;
grant execute on function public.fn_write_draft_sales(uuid, uuid, integer, uuid, numeric, numeric) to pomkita_app;
grant execute on function public.fn_write_draft_loss(uuid, uuid, integer, uuid, text, text, numeric, numeric, text) to pomkita_app;
grant execute on function public.fn_stage_draft_evidence(uuid, uuid, integer, uuid, text, text, bytea, bigint, text) to pomkita_app;
grant execute on function public.read_draft(uuid) to pomkita_app;

insert into public.procedure_registry (name, allowed_roles, action, lock_rank)
values
  ('fn_claim_draft', array['Supervisor'], 'claim_draft', 30),
  ('fn_heartbeat_draft', array['Supervisor'], 'heartbeat_draft', 30),
  ('fn_fence_draft', array[]::text[], 'internal_draft_fence', 30),
  ('fn_write_draft_reading', array['Supervisor'], 'write_draft_reading', 30),
  ('fn_write_draft_sales', array['Supervisor'], 'write_draft_sales', 30),
  ('fn_write_draft_loss', array['Supervisor'], 'write_draft_loss', 30),
  ('fn_stage_draft_evidence', array['Supervisor'], 'stage_draft_evidence', 30),
  ('read_draft', array['Supervisor', 'Station Admin', 'Owner'], 'read_draft', 30);
