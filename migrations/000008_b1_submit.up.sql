-- PLAN.md sections 4.2-4.4, 5, 7, and 8; BE-PLAN.md Phase B1.

create table public.threshold_policy_revisions (
  rev_id uuid primary key default app.gen_random_uuid(), policy_id uuid not null,
  org_id uuid not null, station_id uuid, valid_from timestamptz(6) not null,
  supersedes_org_id uuid, supersedes_rev_id uuid, disabled boolean not null default false,
  loss_liter_threshold numeric(8,2) not null default 10, gain_liter_threshold numeric(8,2) not null default 10,
  loss_rupiah_threshold numeric(14,0) not null default 0, gain_rupiah_threshold numeric(14,0) not null default 0,
  variance_rupiah_threshold numeric(14,0) not null default 0, rollover_threshold numeric(10,1),
  created_by uuid not null, created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, rev_id), foreign key (org_id) references public.organizations(org_id),
  foreign key (org_id, station_id) references public.stations(org_id, station_id),
  foreign key (org_id, created_by) references public.users(org_id, user_id),
  check (loss_liter_threshold >= 0 and gain_liter_threshold >= 0 and loss_rupiah_threshold >= 0 and gain_rupiah_threshold >= 0 and variance_rupiah_threshold >= 0)
);
alter table public.threshold_policy_revisions owner to org_owner;
create unique index threshold_policy_scope_time on public.threshold_policy_revisions(org_id, coalesce(station_id,'00000000-0000-0000-0000-000000000000'::uuid), valid_from);

create table public.evidence_policy_revisions (
  rev_id uuid primary key default app.gen_random_uuid(), policy_id uuid not null,
  org_id uuid not null, station_id uuid, valid_from timestamptz(6) not null,
  supersedes_org_id uuid, supersedes_rev_id uuid, disabled boolean not null default false,
  mode text not null, created_by uuid not null, created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, rev_id), foreign key (org_id) references public.organizations(org_id),
  foreign key (org_id, station_id) references public.stations(org_id, station_id),
  foreign key (org_id, created_by) references public.users(org_id, user_id), check (mode in ('opsional','wajib'))
);
alter table public.evidence_policy_revisions owner to org_owner;
create unique index evidence_policy_scope_time on public.evidence_policy_revisions(org_id, coalesce(station_id,'00000000-0000-0000-0000-000000000000'::uuid), valid_from);

create table public.evidence_policy_types (
  org_id uuid not null, rev_id uuid not null, evidence_type text not null,
  minimum_count_per_loss integer not null, accepted_mime_types text[] not null,
  primary key(org_id,rev_id,evidence_type), foreign key(org_id,rev_id) references public.evidence_policy_revisions(org_id,rev_id),
  check (btrim(evidence_type) <> '' and minimum_count_per_loss >= 0 and cardinality(accepted_mime_types) > 0)
);
alter table public.evidence_policy_types owner to org_owner;

create table public.policy_snapshot_sets (
  set_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null,
  created_at timestamptz(6) not null default clock_timestamp(), unique(org_id,station_id,shift_id,set_id),
  foreign key(org_id,station_id,shift_id) references public.shifts(org_id,station_id,shift_id)
);
alter table public.policy_snapshot_sets owner to report_writer;
create table public.policy_snapshot_items (
  item_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null,
  set_id uuid not null, policy_kind text not null, policy_id uuid not null, rev_id uuid not null, scope text not null,
  payload jsonb not null, payload_hash bytea not null check(octet_length(payload_hash)=32),
  unique(org_id,station_id,shift_id,set_id,policy_kind), foreign key(org_id,station_id,shift_id,set_id) references public.policy_snapshot_sets(org_id,station_id,shift_id,set_id),
  check(policy_kind in ('threshold','evidence') and scope in ('organization','station'))
);
alter table public.policy_snapshot_items owner to report_writer;

create table public.shift_reports (
  report_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null,
  version_no integer not null, supersedes_report_id uuid, status text not null default 'submitted', submitted_by uuid not null,
  submitted_at timestamptz(6) not null default clock_timestamp(), policy_snapshot_set_id uuid not null,
  unique(org_id,station_id,report_id), unique(org_id,station_id,shift_id,report_id), unique(org_id,station_id,shift_id,report_id,version_no), unique(org_id,station_id,shift_id,version_no),
  foreign key(org_id,station_id,shift_id) references public.shifts(org_id,station_id,shift_id),
  foreign key(org_id,submitted_by) references public.users(org_id,user_id), foreign key(org_id,station_id,shift_id,policy_snapshot_set_id) references public.policy_snapshot_sets(org_id,station_id,shift_id,set_id),
  check(version_no > 0 and status in ('submitted','locked'))
);
alter table public.shift_reports owner to report_writer;
alter table public.shift_reports add constraint shift_reports_supersedes_fk foreign key(org_id,station_id,shift_id,supersedes_report_id) references public.shift_reports(org_id,station_id,shift_id,report_id);
alter table public.shifts add constraint shifts_current_report_fk foreign key(org_id,station_id,shift_id,current_report_id) references public.shift_reports(org_id,station_id,shift_id,report_id);

create table public.dispenser_readings (
  reading_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null, report_id uuid not null, nozzle_id uuid not null,
  meter_start numeric(10,1) not null, meter_end numeric(10,1) not null, price_used numeric(14,0) not null, expected_sale_rupiah numeric(14,0) not null,
  observed boolean not null default true, is_carried_forward boolean not null default false, source_shift_id uuid, source_report_id uuid, source_reading_id uuid,
  unique(org_id,station_id,shift_id,report_id,nozzle_id), unique(org_id,station_id,shift_id,report_id,reading_id),
  foreign key(org_id,station_id,shift_id,report_id) references public.shift_reports(org_id,station_id,shift_id,report_id), foreign key(org_id,station_id,nozzle_id) references public.nozzles(org_id,station_id,nozzle_id),
  foreign key(org_id,station_id,source_shift_id,source_report_id,source_reading_id) references public.dispenser_readings(org_id,station_id,shift_id,report_id,reading_id),
  check(meter_start >= 0 and meter_end >= 0 and price_used >= 0 and expected_sale_rupiah >= 0), check((is_carried_forward and source_shift_id is not null and source_report_id is not null and source_reading_id is not null) or (not is_carried_forward and source_shift_id is null and source_report_id is null and source_reading_id is null))
);
alter table public.dispenser_readings owner to report_writer;

create table public.sales_declared (
  sales_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null, report_id uuid not null, dispenser_id uuid not null,
  cash_amount numeric(14,0) not null, cashless_amount numeric(14,0) not null default 0, created_by uuid not null,
  unique(org_id,station_id,shift_id,report_id,dispenser_id), foreign key(org_id,station_id,shift_id,report_id) references public.shift_reports(org_id,station_id,shift_id,report_id), foreign key(org_id,station_id,dispenser_id) references public.dispensers(org_id,station_id,dispenser_id), foreign key(org_id,created_by) references public.users(org_id,user_id), check(cash_amount >= 0 and cashless_amount >= 0)
);
alter table public.sales_declared owner to report_writer;
create table public.loss_identity (loss_id uuid primary key, org_id uuid not null, station_id uuid not null, created_by uuid not null, created_at timestamptz(6) not null default clock_timestamp(), unique(org_id,station_id,loss_id), foreign key(org_id,station_id) references public.stations(org_id,station_id), foreign key(org_id,created_by) references public.users(org_id,user_id));
alter table public.loss_identity owner to report_writer;
create table public.loss_entries (
  row_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null, report_id uuid not null, version_no integer not null, loss_id uuid not null, nozzle_id uuid, direction text not null, reason_code text not null, liters numeric(8,2) not null, cash_amount numeric(14,0), note text, created_by uuid not null,
  unique(org_id,station_id,shift_id,report_id,loss_id), unique(org_id,station_id,shift_id,report_id,row_id), foreign key(org_id,station_id,shift_id,report_id,version_no) references public.shift_reports(org_id,station_id,shift_id,report_id,version_no), foreign key(org_id,station_id,loss_id) references public.loss_identity(org_id,station_id,loss_id), foreign key(org_id,station_id,nozzle_id) references public.nozzles(org_id,station_id,nozzle_id), foreign key(org_id,created_by) references public.users(org_id,user_id), check(direction in ('loss','gain') and liters >= 0 and (cash_amount is null or cash_amount >= 0))
);
alter table public.loss_entries owner to report_writer;
create table public.deliveries (delivery_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null, do_number text not null, tank_id uuid not null, liters numeric(8,2) not null, created_by uuid not null, unique(org_id,station_id,shift_id,delivery_id), foreign key(org_id,station_id,shift_id) references public.shifts(org_id,station_id,shift_id), foreign key(org_id,station_id,tank_id) references public.tanks(org_id,station_id,tank_id), foreign key(org_id,created_by) references public.users(org_id,user_id), check(btrim(do_number) <> '' and liters >= 0));
alter table public.deliveries owner to report_writer;
create table public.dip_readings (dip_id uuid primary key default app.gen_random_uuid(), org_id uuid not null, station_id uuid not null, shift_id uuid not null, tank_id uuid not null, dip_liters numeric(8,2) not null, created_by uuid not null, unique(org_id,station_id,shift_id,dip_id), foreign key(org_id,station_id,shift_id) references public.shifts(org_id,station_id,shift_id), foreign key(org_id,station_id,tank_id) references public.tanks(org_id,station_id,tank_id), foreign key(org_id,created_by) references public.users(org_id,user_id), check(dip_liters >= 0));
alter table public.dip_readings owner to report_writer;

create or replace function public.trg_report_guard() returns trigger language plpgsql security definer set search_path=pg_catalog,public,app as $$ begin if tg_op='DELETE' then raise exception using errcode='23514',message='report_immutable'; end if; if tg_table_name='shift_reports' and current_setting('app.transition',true)='lock_report' and old.status='submitted' and new.status='locked' and (row_to_json(new)::jsonb-'status' = row_to_json(old)::jsonb-'status') then return new; end if; raise exception using errcode='23514',message='report_immutable'; end; $$;
alter function public.trg_report_guard() owner to report_writer;
create trigger shift_reports_guard before update or delete on public.shift_reports for each row execute function public.trg_report_guard();
do $$ declare t text; begin foreach t in array array['dispenser_readings','sales_declared','loss_identity','loss_entries','deliveries','dip_readings'] loop execute format('create trigger %I_guard before update or delete on public.%I for each row execute function public.trg_report_guard()',t,t); end loop; end $$;

create or replace function public.trg_shift_guard() returns trigger language plpgsql security definer set search_path=pg_catalog,public,app as $$ begin if tg_op='DELETE' then raise exception using errcode='23514',message='shift_delete_forbidden'; end if; if tg_table_name='shift_transitions' then if current_setting('app.transition',true) not in('shift_transition','submit_shift') then raise exception using errcode='23514',message='transition_insert_forbidden';end if;return new;end if;if new.station_seq<>old.station_seq or new.shift_price_map_snapshot<>old.shift_price_map_snapshot or new.shift_price_map_hash<>old.shift_price_map_hash then raise exception using errcode='23514',message='shift_immutable_fields';end if;if new.current_report_id is distinct from old.current_report_id and current_setting('app.transition',true)<>'submit_shift' then raise exception using errcode='23514',message='current_report_pointer_forbidden';end if;if new.status<>old.status and current_setting('app.transition',true) not in('shift_transition','submit_shift') then raise exception using errcode='23514',message='shift_transition_required';end if;return new;end; $$;

do $$ declare t text; begin foreach t in array array['threshold_policy_revisions','evidence_policy_revisions','policy_snapshot_sets','policy_snapshot_items','shift_reports','dispenser_readings','sales_declared','loss_identity','loss_entries','deliveries','dip_readings'] loop execute format('alter table public.%I enable row level security',t); execute format('alter table public.%I force row level security',t); execute format('create policy %I_context on public.%I using(org_id::text=current_setting(''app.org_id'',true) and (current_setting(''app.station_id'',true)='''' or station_id is null or station_id::text=current_setting(''app.station_id'',true))) with check(org_id::text=current_setting(''app.org_id'',true) and (current_setting(''app.station_id'',true)='''' or station_id is null or station_id::text=current_setting(''app.station_id'',true)))',t,t); end loop; alter table public.evidence_policy_types enable row level security; alter table public.evidence_policy_types force row level security; create policy evidence_policy_types_context on public.evidence_policy_types using(exists(select 1 from public.evidence_policy_revisions e where e.org_id=evidence_policy_types.org_id and e.rev_id=evidence_policy_types.rev_id)); end $$;

create or replace function public.fn_submit_shift(p_shift_id uuid,p_draft_id uuid,p_claim_token uuid,p_expected_revision integer,p_idempotency_key text,p_request jsonb) returns table(report_id uuid,replay boolean,request_hash bytea) language plpgsql security definer set search_path=pg_catalog,public,app as $$
declare o uuid:=nullif(current_setting('app.org_id',true),'')::uuid; a uuid:=nullif(current_setting('app.user_id',true),'')::uuid; st uuid; s public.shift_status; d integer; h bytea:=app.digest(p_request::text,'sha256'); idem uuid; oldtok uuid; ist text; ireport uuid; lease timestamptz(6); claim uuid:=app.gen_random_uuid(); setid uuid:=app.gen_random_uuid(); rid uuid:=app.gen_random_uuid(); th public.threshold_policy_revisions%rowtype; ev public.evidence_policy_revisions%rowtype; x jsonb; item jsonb; ms numeric; me numeric; delta numeric; price numeric; modulus numeric; expected numeric; prev numeric;
begin
 if current_setting('app.context_valid',true)<>'true' then raise exception using errcode='28000',message='request_context_required'; end if;
 select station_id,status into st,s from public.shifts where org_id=o and shift_id=p_shift_id; if not found then raise exception using errcode='42501',message='shift_not_found'; end if;
 perform 1 from public.stations where org_id=o and station_id=st for update;
 select status into s from public.shifts where org_id=o and station_id=st and shift_id=p_shift_id for update;
 select revision into d from public.shift_drafts where org_id=o and station_id=st and draft_id=p_draft_id and shift_id=p_shift_id for update; if not found then raise exception using errcode='42501',message='draft_not_found'; end if;
 insert into public.submit_idempotency(org_id,station_id,shift_id,idempotency_key,request_hash,claim_token,lease_expires_at) values(o,st,p_shift_id,p_idempotency_key,h,claim,clock_timestamp()+interval '10 minutes') on conflict(org_id,station_id,shift_id,idempotency_key) do nothing returning idem_id into idem;
 if idem is null then select i.idem_id,i.request_hash,i.status,i.resulting_report_id,i.claim_token,i.lease_expires_at into idem,request_hash,ist,ireport,oldtok,lease from public.submit_idempotency i where i.org_id=o and i.station_id=st and i.shift_id=p_shift_id and i.idempotency_key=p_idempotency_key for update; if request_hash<>h then raise exception using errcode='23505',message='idempotency_hash_mismatch'; end if; if ist='succeeded' then report_id:=ireport;replay:=true;return next;return;end if; if ist='failed' or lease>=clock_timestamp() then raise exception using errcode='23505',message='idempotency_in_progress';end if; update public.submit_idempotency set claim_token=claim,lease_expires_at=clock_timestamp()+interval '10 minutes',attempt_count=attempt_count+1 where idem_id=idem and claim_token=oldtok and lease_expires_at<clock_timestamp(); if not found then raise exception using errcode='23505',message='idempotency_takeover_conflict';end if; else request_hash:=h; end if;
 perform set_config('app.transition','submit_shift',true); update public.shift_drafts set status='submitting',revision=revision+1,updated_at=clock_timestamp() where org_id=o and station_id=st and draft_id=p_draft_id and claim_token=p_claim_token and claim_expires_at>clock_timestamp() and revision=p_expected_revision and status='editing'; if not found then raise exception using errcode='23505',message='draft_fence_conflict';end if;
 select * into th from public.threshold_policy_revisions t where t.org_id=o and t.valid_from<=clock_timestamp() and (t.station_id=st or t.station_id is null) order by(t.station_id is not null) desc,t.valid_from desc limit 1; select * into ev from public.evidence_policy_revisions e where e.org_id=o and e.valid_from<=clock_timestamp() and(e.station_id=st or e.station_id is null) order by(e.station_id is not null) desc,e.valid_from desc limit 1; if th.rev_id is null or ev.rev_id is null then raise exception using errcode='23514',message='policy_revision_missing';end if;
 insert into public.policy_snapshot_sets(set_id,org_id,station_id,shift_id) values(setid,o,st,p_shift_id); insert into public.policy_snapshot_items(org_id,station_id,shift_id,set_id,policy_kind,policy_id,rev_id,scope,payload,payload_hash) values(o,st,p_shift_id,setid,'threshold',th.policy_id,th.rev_id,case when th.station_id is null then 'organization' else 'station' end,jsonb_build_object('hash_version',1,'loss_liter_threshold',th.loss_liter_threshold::text,'gain_liter_threshold',th.gain_liter_threshold::text,'loss_rupiah_threshold',th.loss_rupiah_threshold::text,'gain_rupiah_threshold',th.gain_rupiah_threshold::text,'variance_rupiah_threshold',th.variance_rupiah_threshold::text,'rollover_threshold',coalesce(th.rollover_threshold,0)::text),app.digest(th.rev_id::text,'sha256')); insert into public.policy_snapshot_items(org_id,station_id,shift_id,set_id,policy_kind,policy_id,rev_id,scope,payload,payload_hash) values(o,st,p_shift_id,setid,'evidence',ev.policy_id,ev.rev_id,case when ev.station_id is null then 'organization' else 'station' end,jsonb_build_object('hash_version',1,'mode',ev.mode),app.digest(ev.rev_id::text,'sha256'));
 insert into public.shift_reports(report_id,org_id,station_id,shift_id,version_no,status,submitted_by,policy_snapshot_set_id) values(rid,o,st,p_shift_id,1,'submitted',a,setid);
 for x in select jsonb_array_elements(coalesce(p_request->'readings','[]'::jsonb)) loop select i into item from jsonb_array_elements((select shift_price_map_snapshot->'items' from public.shifts where shift_id=p_shift_id)) i where i->>'nozzle_id'=x->>'nozzle_id'; if item is null then raise exception using errcode='23514',message='snapshot_nozzle_missing';end if; ms:=(x->>'meter_start')::numeric;me:=(x->>'meter_end')::numeric;price:=(item->>'price')::numeric;modulus:=(item->>'modulus')::numeric;if me>=ms then delta:=me-ms;else delta:=me-ms+modulus;if delta>coalesce(nullif(th.rollover_threshold,0),modulus*.2) then raise exception using errcode='23514',message='rollover_over_threshold';end if;end if;expected:=round(delta*price,0);if expected>99999999999999 then raise exception using errcode='22003',message='money_overflow';end if;insert into public.dispenser_readings(org_id,station_id,shift_id,report_id,nozzle_id,meter_start,meter_end,price_used,expected_sale_rupiah,observed) values(o,st,p_shift_id,rid,(x->>'nozzle_id')::uuid,ms,me,price,expected,coalesce((x->>'observed')::boolean,true));end loop;
 for x in select jsonb_array_elements(coalesce(p_request->'sales','[]'::jsonb)) loop insert into public.sales_declared(org_id,station_id,shift_id,report_id,dispenser_id,cash_amount,cashless_amount,created_by) values(o,st,p_shift_id,rid,(x->>'dispenser_id')::uuid,(x->>'cash_amount')::numeric,coalesce((x->>'cashless_amount')::numeric,0),a);end loop;
 for x in select jsonb_array_elements(coalesce(p_request->'losses','[]'::jsonb)) loop insert into public.loss_identity(loss_id,org_id,station_id,created_by) values((x->>'loss_id')::uuid,o,st,a) on conflict do nothing;insert into public.loss_entries(org_id,station_id,shift_id,report_id,version_no,loss_id,nozzle_id,direction,reason_code,liters,cash_amount,note,created_by) values(o,st,p_shift_id,rid,1,(x->>'loss_id')::uuid,nullif(x->>'nozzle_id','')::uuid,x->>'direction',x->>'reason_code',(x->>'liters')::numeric,nullif(x->>'cash_amount','')::numeric,x->>'note',a);end loop;
 update public.shifts set status='awaiting_confirmation',current_report_id=rid where org_id=o and station_id=st and shift_id=p_shift_id; insert into public.shift_transitions(org_id,station_id,shift_id,from_status,to_status,actor_user_id,reason) values(o,st,p_shift_id,s,'awaiting_confirmation',a,'submit');update public.shift_drafts set status='submitted' where org_id=o and station_id=st and draft_id=p_draft_id and claim_token=p_claim_token;update public.submit_idempotency set status='succeeded',resulting_report_id=rid where idem_id=idem and claim_token=claim;report_id:=rid;replay:=false;request_hash:=h;return next;
end; $$;
alter function public.fn_submit_shift(uuid,uuid,uuid,integer,text,jsonb) owner to report_writer;
grant usage on schema public,app to report_writer; grant select on public.stations,public.shifts,public.shift_drafts,public.nozzles,public.dispenser_prices,public.dispenser_nozzle_map,public.nozzle_tank_map,public.threshold_policy_revisions,public.evidence_policy_revisions,public.evidence_policy_types,public.dispenser_readings to report_writer; grant update,insert on public.shifts,public.shift_drafts,public.submit_idempotency to report_writer; grant update on public.stations to report_writer; grant all on public.submit_idempotency to report_writer; grant insert on public.shift_transitions to report_writer; grant execute on function public.fn_submit_shift(uuid,uuid,uuid,integer,text,jsonb) to pomkita_app;
insert into public.procedure_registry(name,allowed_roles,action,lock_rank) values('fn_submit_shift',array['Supervisor'],'submit_shift',50),('trg_report_guard',array[]::text[],'internal_trigger',50) on conflict(name) do update set allowed_roles=excluded.allowed_roles,action=excluded.action,lock_rank=excluded.lock_rank;
