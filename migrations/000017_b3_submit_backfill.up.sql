-- PLAN.md sections 3.1, 4.2, 5.1, 5.4, 6, 7, and 8; BE-PLAN.md Phase B3.

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
  select timezone into v_timezone from public.stations
   where org_id = v_org_id and station_id = p_station_id for update;
  if not found then
    raise exception using errcode = '42501', message = 'station_not_found';
  end if;
  select coalesce(max(s.station_seq), 0) + 1 into v_seq
    from public.shifts s where s.org_id = v_org_id and s.station_id = p_station_id;
  if p_backfilled and exists (
    select 1
      from public.shifts s
     where s.org_id = v_org_id and s.station_id = p_station_id
       and s.status = 'locked'
       and coalesce(s.original_event_date, s.business_date) > p_original_event_date
       and exists (
         select 1 from public.dispenser_readings d
          where d.org_id = s.org_id and d.station_id = s.station_id
            and d.shift_id = s.shift_id
            and d.source_shift_id is not null)) then
    raise exception using errcode = '23505', message = 'backfill_out_of_order';
  end if;
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
           ) order by n.nozzle_id))
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
  if p_backfilled and (
    p_original_event_date is null or p_shift_ke is null or p_backfill_approver is null
    or btrim(coalesce(p_backfill_reason, '')) = ''
    or current_setting('app.role', true) not in ('Supervisor', 'Owner', 'Superadmin')
    or not exists (
      select 1 from public.user_station_roles r
       where r.org_id = v_org_id and r.station_id = p_station_id
         and r.user_id = p_backfill_approver and r.role in ('Owner', 'Superadmin'))) then
    raise exception using errcode = '23514', message = 'backfill_approval_required';
  end if;
  if not p_backfilled and (p_original_event_date is not null or p_shift_ke is not null
      or p_backfill_approver is not null or p_backfill_reason is not null) then
    raise exception using errcode = '23514', message = 'unexpected_backfill_fields';
  end if;
  v_hash := app.digest(v_snapshot::text, 'sha256');
  v_shift_id := app.gen_random_uuid();
  insert into public.shifts(
    shift_id, org_id, station_id, station_seq, supervisor_id, opened_at, timezone_snapshot,
    business_date, original_event_date, shift_ke, backfilled, backfill_approver,
    backfill_approved_at, backfill_reason, shift_price_map_snapshot, shift_price_map_hash)
  values(
    v_shift_id, v_org_id, p_station_id, v_seq, p_supervisor_id, p_opened_at, v_timezone,
    (p_opened_at at time zone v_timezone)::date, p_original_event_date, p_shift_ke,
    p_backfilled, p_backfill_approver, case when p_backfilled then clock_timestamp() end,
    p_backfill_reason, v_snapshot, v_hash);
  insert into public.shift_drafts(org_id, station_id, shift_id, owned_by, updated_by)
  values(v_org_id, p_station_id, v_shift_id, p_supervisor_id, p_supervisor_id);
  return query select v_shift_id, v_seq, (p_opened_at at time zone v_timezone)::date, v_snapshot, v_hash;
end;
$$;
alter function public.fn_open_shift(uuid,uuid,timestamptz,boolean,date,integer,uuid,text) owner to station_owner;

create or replace function public.fn_submit_shift(
  p_shift_id uuid,
  p_draft_id uuid,
  p_claim_token uuid,
  p_expected_revision integer,
  p_idempotency_key text,
  p_request jsonb
)
returns table (report_id uuid, replay boolean, request_hash bytea)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  o uuid := nullif(current_setting('app.org_id', true), '')::uuid;
  a uuid := nullif(current_setting('app.user_id', true), '')::uuid;
  st uuid;
  s public.shift_status;
  d integer;
  h bytea := app.digest(p_request::text, 'sha256');
  idem uuid;
  oldtok uuid;
  ist text;
  ireport uuid;
  lease timestamptz(6);
  claim uuid := app.gen_random_uuid();
  setid uuid := app.gen_random_uuid();
  rid uuid := app.gen_random_uuid();
  th public.threshold_policy_revisions%rowtype;
  ev public.evidence_policy_revisions%rowtype;
  x jsonb;
  item jsonb;
  ms numeric;
  me numeric;
  delta numeric;
  price numeric;
  modulus numeric;
  expected numeric;
  v_backfilled boolean;
  v_station_seq bigint;
  v_source record;
begin
  if current_setting('app.context_valid', true) <> 'true' then
    raise exception using errcode = '28000', message = 'request_context_required';
  end if;
  select station_id, status, backfilled, station_seq into st, s, v_backfilled, v_station_seq
    from public.shifts where org_id = o and shift_id = p_shift_id;
  if not found then
    raise exception using errcode = '42501', message = 'shift_not_found';
  end if;
  perform 1 from public.stations where org_id = o and station_id = st for update;
  select status into s from public.shifts where org_id = o and station_id = st and shift_id = p_shift_id for update;
  select revision into d from public.shift_drafts
   where org_id = o and station_id = st and draft_id = p_draft_id and shift_id = p_shift_id for update;
  if not found then
    raise exception using errcode = '42501', message = 'draft_not_found';
  end if;
  insert into public.submit_idempotency(org_id, station_id, shift_id, idempotency_key, request_hash, claim_token, lease_expires_at)
  values(o, st, p_shift_id, p_idempotency_key, h, claim, clock_timestamp() + interval '10 minutes')
  on conflict(org_id, station_id, shift_id, idempotency_key) do nothing returning idem_id into idem;
  if idem is null then
    select i.idem_id, i.request_hash, i.status, i.resulting_report_id, i.claim_token, i.lease_expires_at
      into idem, request_hash, ist, ireport, oldtok, lease
      from public.submit_idempotency i
     where i.org_id = o and i.station_id = st and i.shift_id = p_shift_id
       and i.idempotency_key = p_idempotency_key for update;
    if request_hash <> h then
      raise exception using errcode = '23505', message = 'idempotency_hash_mismatch';
    end if;
    if ist = 'succeeded' then
      report_id := ireport; replay := true; return next; return;
    end if;
    if ist = 'failed' or lease >= clock_timestamp() then
      raise exception using errcode = '23505', message = 'idempotency_in_progress';
    end if;
    update public.submit_idempotency set claim_token = claim, lease_expires_at = clock_timestamp() + interval '10 minutes', attempt_count = attempt_count + 1
     where idem_id = idem and claim_token = oldtok and lease_expires_at < clock_timestamp();
    if not found then
      raise exception using errcode = '23505', message = 'idempotency_takeover_conflict';
    end if;
  else
    request_hash := h;
  end if;
  perform set_config('app.transition', 'submit_shift', true);
  update public.shift_drafts set status = 'submitting', revision = revision + 1, updated_at = clock_timestamp()
   where org_id = o and station_id = st and draft_id = p_draft_id and claim_token = p_claim_token
     and claim_expires_at > clock_timestamp() and revision = p_expected_revision and status = 'editing';
  if not found then
    raise exception using errcode = '23505', message = 'draft_fence_conflict';
  end if;
  select * into th from public.threshold_policy_revisions t
   where t.org_id = o and t.valid_from <= clock_timestamp() and (t.station_id = st or t.station_id is null)
   order by (t.station_id is not null) desc, t.valid_from desc limit 1;
  select * into ev from public.evidence_policy_revisions e
   where e.org_id = o and e.valid_from <= clock_timestamp() and (e.station_id = st or e.station_id is null)
   order by (e.station_id is not null) desc, e.valid_from desc limit 1;
  if th.rev_id is null or ev.rev_id is null then
    raise exception using errcode = '23514', message = 'policy_revision_missing';
  end if;
  insert into public.policy_snapshot_sets(set_id, org_id, station_id, shift_id) values(setid, o, st, p_shift_id);
  insert into public.policy_snapshot_items(org_id, station_id, shift_id, set_id, policy_kind, policy_id, rev_id, scope, payload, payload_hash)
  values(o, st, p_shift_id, setid, 'threshold', th.policy_id, th.rev_id,
    case when th.station_id is null then 'organization' else 'station' end,
    jsonb_build_object('hash_version',1,'loss_liter_threshold',th.loss_liter_threshold::text,'gain_liter_threshold',th.gain_liter_threshold::text,'loss_rupiah_threshold',th.loss_rupiah_threshold::text,'gain_rupiah_threshold',th.gain_rupiah_threshold::text,'variance_rupiah_threshold',th.variance_rupiah_threshold::text,'rollover_threshold',coalesce(th.rollover_threshold,0)::text), app.digest(th.rev_id::text,'sha256'));
  insert into public.policy_snapshot_items(org_id, station_id, shift_id, set_id, policy_kind, policy_id, rev_id, scope, payload, payload_hash)
  values(o, st, p_shift_id, setid, 'evidence', ev.policy_id, ev.rev_id,
    case when ev.station_id is null then 'organization' else 'station' end,
    jsonb_build_object('hash_version',1,'mode',ev.mode,'types',coalesce((select jsonb_agg(jsonb_build_object('type',t.evidence_type,'minimum_count_per_loss',t.minimum_count_per_loss,'accepted_mime_types',to_jsonb(t.accepted_mime_types)) order by t.evidence_type) from public.evidence_policy_types t where t.org_id=o and t.rev_id=ev.rev_id),'[]'::jsonb)), app.digest(ev.rev_id::text,'sha256'));
  insert into public.shift_reports(report_id, org_id, station_id, shift_id, version_no, status, submitted_by, policy_snapshot_set_id)
  values(rid, o, st, p_shift_id, 1, 'submitted', a, setid);
  if v_backfilled then
    for item in select value from jsonb_array_elements((select shift_price_map_snapshot->'items' from public.shifts where shift_id=p_shift_id)) loop
      select dr.* into v_source
        from public.shifts ps
        join public.shift_reports pr on pr.org_id=ps.org_id and pr.station_id=ps.station_id and pr.shift_id=ps.shift_id and pr.report_id=ps.current_report_id
        join public.dispenser_readings dr on dr.org_id=pr.org_id and dr.station_id=pr.station_id and dr.shift_id=pr.shift_id and dr.report_id=pr.report_id
       where ps.org_id=o and ps.station_id=st and ps.status='locked' and ps.station_seq < v_station_seq
         and dr.nozzle_id=(item->>'nozzle_id')::uuid
       order by ps.station_seq desc limit 1;
      if not found then
        raise exception using errcode = '23514', message = 'backfill_meter_source_missing';
      end if;
      insert into public.dispenser_readings(org_id,station_id,shift_id,report_id,nozzle_id,meter_start,meter_end,price_used,expected_sale_rupiah,observed,is_carried_forward,source_shift_id,source_report_id,source_reading_id)
      values(o,st,p_shift_id,rid,v_source.nozzle_id,v_source.meter_start,v_source.meter_end,v_source.price_used,v_source.expected_sale_rupiah,false,true,v_source.shift_id,v_source.report_id,v_source.reading_id);
    end loop;
  else
    for x in select jsonb_array_elements(coalesce(p_request->'readings','[]'::jsonb)) loop
      select i into item from jsonb_array_elements((select shift_price_map_snapshot->'items' from public.shifts where shift_id=p_shift_id)) i where i->>'nozzle_id'=x->>'nozzle_id';
      if item is null then raise exception using errcode='23514',message='snapshot_nozzle_missing'; end if;
      ms := (x->>'meter_start')::numeric; me := (x->>'meter_end')::numeric; price := (item->>'price')::numeric; modulus := (item->>'modulus')::numeric;
      if me >= ms then delta := me-ms; else delta := me-ms+modulus; if delta > coalesce(nullif(th.rollover_threshold,0),modulus*.2) then raise exception using errcode='23514',message='rollover_over_threshold'; end if; end if;
      expected := round(delta*price,0); if expected > 99999999999999 then raise exception using errcode='22003',message='money_overflow'; end if;
      insert into public.dispenser_readings(org_id,station_id,shift_id,report_id,nozzle_id,meter_start,meter_end,price_used,expected_sale_rupiah,observed) values(o,st,p_shift_id,rid,(x->>'nozzle_id')::uuid,ms,me,price,expected,coalesce((x->>'observed')::boolean,true));
    end loop;
  end if;
  for x in select jsonb_array_elements(coalesce(p_request->'sales','[]'::jsonb)) loop
    insert into public.sales_declared(org_id,station_id,shift_id,report_id,dispenser_id,cash_amount,cashless_amount,created_by) values(o,st,p_shift_id,rid,(x->>'dispenser_id')::uuid,(x->>'cash_amount')::numeric,coalesce((x->>'cashless_amount')::numeric,0),a);
  end loop;
  for x in select jsonb_array_elements(coalesce(p_request->'losses','[]'::jsonb)) loop
    insert into public.loss_identity(loss_id,org_id,station_id,created_by) values((x->>'loss_id')::uuid,o,st,a) on conflict do nothing;
    insert into public.loss_entries(org_id,station_id,shift_id,report_id,version_no,loss_id,nozzle_id,direction,reason_code,liters,cash_amount,note,created_by) values(o,st,p_shift_id,rid,1,(x->>'loss_id')::uuid,nullif(x->>'nozzle_id','')::uuid,x->>'direction',x->>'reason_code',(x->>'liters')::numeric,nullif(x->>'cash_amount','')::numeric,x->>'note',a);
  end loop;
  insert into public.evidence_event(evidence_id,org_id,station_id,shift_id,report_id,loss_row_id,event_seq,event_type,evidence_type,object_key,content_hash,size_bytes,mime,actor_user_id)
  select e.row_id,o,st,p_shift_id,rid,l.row_id,1,e.status,e.evidence_type,e.object_key,e.content_hash,e.size_bytes,e.mime,a
    from public.draft_evidence_staging e
    join public.draft_losses dl on dl.org_id=e.org_id and dl.station_id=e.station_id and dl.draft_id=e.draft_id and dl.row_id=e.loss_row_id
    join public.loss_entries l on l.org_id=o and l.station_id=st and l.shift_id=p_shift_id and l.report_id=rid and l.loss_id=dl.loss_id
   where e.org_id=o and e.station_id=st and e.draft_id=p_draft_id;
  for x in select jsonb_array_elements(coalesce(p_request->'exceptions','[]'::jsonb)) loop
    insert into public.loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id)
    values(app.gen_random_uuid(),o,st,p_shift_id,rid,(x->>'loss_row_id')::uuid,x->>'reason',a);
  end loop;
  for x in select jsonb_array_elements(coalesce(p_request->'losses','[]'::jsonb)) loop
    if btrim(coalesce(x->>'exception_reason','')) <> '' then
      insert into public.loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id)
      select app.gen_random_uuid(),o,st,p_shift_id,rid,l.row_id,x->>'exception_reason',a
        from public.loss_entries l where l.org_id=o and l.station_id=st and l.shift_id=p_shift_id and l.report_id=rid and l.loss_id=(x->>'loss_id')::uuid
      on conflict do nothing;
    end if;
  end loop;
  perform public.fn_validate_evidence(rid);
  update public.shifts set status='awaiting_confirmation',current_report_id=rid where org_id=o and station_id=st and shift_id=p_shift_id;
  insert into public.shift_transitions(org_id,station_id,shift_id,from_status,to_status,actor_user_id,reason) values(o,st,p_shift_id,s,'awaiting_confirmation',a,'submit');
  update public.shift_drafts set status='submitted' where org_id=o and station_id=st and draft_id=p_draft_id and claim_token=p_claim_token;
  update public.submit_idempotency set status='succeeded',resulting_report_id=rid where idem_id=idem and claim_token=claim;
  report_id:=rid; replay:=false; request_hash:=h; return next;
end;
$$;
alter function public.fn_submit_shift(uuid,uuid,uuid,integer,text,jsonb) owner to report_writer;
grant select on public.dispenser_readings to station_owner;
grant select on public.user_station_roles to station_owner;
grant select on public.draft_evidence_staging, public.draft_losses to report_writer;

insert into public.procedure_registry(name, allowed_roles, action, lock_rank)
values ('fn_open_shift', array['Supervisor','Owner'], 'open_shift', 10),
       ('fn_submit_shift', array['Supervisor'], 'submit_shift', 50)
on conflict (name) do update set allowed_roles=excluded.allowed_roles, action=excluded.action, lock_rank=excluded.lock_rank;
