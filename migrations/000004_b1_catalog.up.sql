-- PLAN.md sections 4.1, 4.3, 5.2, 7, and 8; BE-PLAN.md Phase B1.

create extension if not exists btree_gist;

create table public.dispensers (
  org_id uuid not null,
  station_id uuid not null,
  dispenser_id uuid not null default app.gen_random_uuid(),
  primary key (org_id, station_id, dispenser_id),
  foreign key (org_id, station_id) references public.stations (org_id, station_id)
);
alter table public.dispensers owner to station_owner;

create table public.tanks (
  org_id uuid not null,
  station_id uuid not null,
  tank_id uuid not null default app.gen_random_uuid(),
  primary key (org_id, station_id, tank_id),
  foreign key (org_id, station_id) references public.stations (org_id, station_id)
);
alter table public.tanks owner to station_owner;

create table public.nozzles (
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null default app.gen_random_uuid(),
  dispenser_id uuid not null,
  meter_max numeric(10,1) not null,
  primary key (org_id, station_id, nozzle_id),
  foreign key (org_id, station_id, dispenser_id)
    references public.dispensers (org_id, station_id, dispenser_id),
  check (meter_max >= 0)
);
alter table public.nozzles owner to station_owner;

create table public.nozzle_tank_map (
  map_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null,
  tank_id uuid not null,
  valid_period tstzrange not null,
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, station_id, tank_id)
    references public.tanks (org_id, station_id, tank_id),
  check (not isempty(valid_period))
);
alter table public.nozzle_tank_map owner to station_owner;
alter table public.nozzle_tank_map
  add constraint nozzle_tank_map_no_overlap
  exclude using gist (
    org_id with =, station_id with =, nozzle_id with =, valid_period with &&
  );

create table public.dispenser_nozzle_map (
  map_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  dispenser_id uuid not null,
  nozzle_id uuid not null,
  valid_period tstzrange not null,
  foreign key (org_id, station_id, dispenser_id)
    references public.dispensers (org_id, station_id, dispenser_id),
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  check (not isempty(valid_period))
);
alter table public.dispenser_nozzle_map owner to station_owner;
alter table public.dispenser_nozzle_map
  add constraint dispenser_nozzle_map_dispenser_no_overlap
  exclude using gist (
    org_id with =, station_id with =, dispenser_id with =, valid_period with &&
  );
alter table public.dispenser_nozzle_map
  add constraint dispenser_nozzle_map_nozzle_no_overlap
  exclude using gist (
    org_id with =, station_id with =, nozzle_id with =, valid_period with &&
  );

create table public.dispenser_prices (
  price_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null,
  price numeric(14,0) not null,
  valid_period tstzrange not null,
  created_by uuid not null,
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, created_by)
    references public.users (org_id, user_id),
  check (price >= 0),
  check (not isempty(valid_period))
);
alter table public.dispenser_prices owner to station_owner;
alter table public.dispenser_prices
  add constraint dispenser_prices_no_overlap
  exclude using gist (
    org_id with =, station_id with =, nozzle_id with =, valid_period with &&
  );

create table public.meter_reset_events (
  reset_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null,
  old_value numeric(10,1) not null,
  new_value numeric(10,1) not null,
  effective_shift_id uuid not null,
  reason text not null,
  actor_user_id uuid not null,
  approver_user_id uuid,
  approved_at timestamptz(6),
  status text not null default 'pending',
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, actor_user_id)
    references public.users (org_id, user_id),
  foreign key (org_id, approver_user_id)
    references public.users (org_id, user_id),
  check (old_value >= 0),
  check (new_value >= 0),
  check (btrim(reason) <> ''),
  check (status in ('pending', 'approved')),
  check ((status = 'pending' and approved_at is null) or (status = 'approved' and approved_at is not null)),
  check (approver_user_id is null or approver_user_id <> actor_user_id)
);
alter table public.meter_reset_events owner to station_owner;

create unique index meter_reset_events_approved_key
  on public.meter_reset_events (org_id, station_id, nozzle_id, effective_shift_id)
  where status = 'approved';

create table public.nozzle_baseline_revisions (
  baseline_rev_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null,
  effective_station_seq bigint not null default 0,
  initial_meter_value numeric(10,1) not null,
  meter_max numeric(10,1) not null,
  created_by uuid not null,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, station_id, nozzle_id, baseline_rev_id),
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, created_by)
    references public.users (org_id, user_id),
  check (effective_station_seq >= 0),
  check (initial_meter_value >= 0 and initial_meter_value <= meter_max),
  check (meter_max >= 0)
);
alter table public.nozzle_baseline_revisions owner to station_owner;

create table public.nozzle_baseline_current (
  org_id uuid not null,
  station_id uuid not null,
  nozzle_id uuid not null,
  current_baseline_rev_id uuid not null,
  primary key (org_id, station_id, nozzle_id),
  foreign key (org_id, station_id, nozzle_id)
    references public.nozzles (org_id, station_id, nozzle_id),
  foreign key (org_id, station_id, nozzle_id, current_baseline_rev_id)
    references public.nozzle_baseline_revisions
      (org_id, station_id, nozzle_id, baseline_rev_id)
);
alter table public.nozzle_baseline_current owner to station_owner;

do $$
declare
  v_table text;
begin
  foreach v_table in array array[
    'dispensers', 'tanks', 'nozzles', 'nozzle_tank_map',
    'dispenser_nozzle_map', 'dispenser_prices', 'meter_reset_events',
    'nozzle_baseline_revisions', 'nozzle_baseline_current'
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

revoke all on table public.dispensers, public.tanks, public.nozzles,
  public.nozzle_tank_map, public.dispenser_nozzle_map, public.dispenser_prices,
  public.meter_reset_events, public.nozzle_baseline_revisions,
  public.nozzle_baseline_current from public, pomkita_app, report_writer,
  audit_owner, relay;
revoke all on all sequences in schema public from public, pomkita_app, report_writer,
  audit_owner, relay;
