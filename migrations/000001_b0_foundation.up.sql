-- PLAN.md sections 4.1 and 7; BE-PLAN.md Phase B0.

create schema if not exists app;
select pg_advisory_xact_lock(hashtext('pomkita:extension:pgcrypto'));
create extension if not exists pgcrypto with schema app;

do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'pomkita_app') then
    create role pomkita_app nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'report_writer') then
    create role report_writer nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'audit_owner') then
    create role audit_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'relay') then
    create role relay nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'org_owner') then
    create role org_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'station_owner') then
    create role station_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'user_owner') then
    create role user_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'auth_owner') then
    create role auth_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'registry_owner') then
    create role registry_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'audit_lock_owner') then
    create role audit_lock_owner nologin nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls;
  end if;
end
$$;

revoke all on schema public from public;
grant usage on schema public to pomkita_app;
grant usage on schema public to org_owner, station_owner, user_owner, auth_owner, registry_owner, audit_lock_owner;
grant usage on schema app to org_owner, station_owner, user_owner, auth_owner, registry_owner, audit_lock_owner;
grant execute on function app.hmac(text, text, text) to auth_owner;

create table public.organizations (
  org_id uuid primary key default app.gen_random_uuid(),
  name text not null,
  created_at timestamptz(6) not null default clock_timestamp()
);
alter table public.organizations owner to org_owner;

create table public.stations (
  org_id uuid not null,
  station_id uuid not null default app.gen_random_uuid(),
  timezone text not null,
  created_at timestamptz(6) not null default clock_timestamp(),
  primary key (org_id, station_id),
  foreign key (org_id) references public.organizations (org_id)
);
alter table public.stations owner to station_owner;

create table public.users (
  user_id uuid primary key default app.gen_random_uuid(),
  org_id uuid not null,
  display_name text not null,
  enabled boolean not null default true,
  created_at timestamptz(6) not null default clock_timestamp(),
  unique (org_id, user_id),
  foreign key (org_id) references public.organizations (org_id)
);
alter table public.users owner to user_owner;

create table public.user_station_roles (
  org_id uuid not null,
  station_id uuid not null,
  user_id uuid not null,
  role text not null,
  primary key (org_id, station_id, user_id, role),
  foreign key (org_id, station_id) references public.stations (org_id, station_id),
  foreign key (org_id, user_id) references public.users (org_id, user_id),
  check (role in ('Operator', 'Supervisor', 'Station Admin', 'Owner', 'Superadmin'))
);
alter table public.user_station_roles owner to user_owner;

create table public.sessions (
  jti uuid primary key default app.gen_random_uuid(),
  kid text not null,
  issued_at timestamptz(6) not null,
  expires_at timestamptz(6) not null,
  revoked_at timestamptz(6),
  check (expires_at >= issued_at),
  check (expires_at - issued_at <= interval '15 minutes')
);
alter table public.sessions owner to auth_owner;

create table public.jwt_keys (
  kid text primary key,
  secret_ref text not null,
  status text not null,
  activated_at timestamptz(6) not null,
  retired_at timestamptz(6),
  max_token_expiry timestamptz(6) not null,
  check (status in ('active', 'previous', 'retired')),
  check (retired_at is null or retired_at >= activated_at),
  check (max_token_expiry >= activated_at)
);
alter table public.jwt_keys owner to auth_owner;

create table public.procedure_registry (
  name text primary key,
  allowed_roles text[] not null,
  action text not null,
  lock_rank integer not null,
  enabled boolean not null default true
);
alter table public.procedure_registry owner to registry_owner;

create table public.audit_chain_locks (
  org_id uuid primary key,
  foreign key (org_id) references public.organizations (org_id)
);
alter table public.audit_chain_locks owner to audit_lock_owner;

alter table public.organizations enable row level security;
alter table public.organizations force row level security;
create policy organizations_context on public.organizations
  using (org_id::text = current_setting('app.org_id', true));

alter table public.stations enable row level security;
alter table public.stations force row level security;
create policy stations_context on public.stations
  using (org_id::text = current_setting('app.org_id', true));

alter table public.users enable row level security;
alter table public.users force row level security;
create policy users_context on public.users
  using (org_id::text = current_setting('app.org_id', true));
create policy users_authentication on public.users to auth_owner
  using (true);

alter table public.user_station_roles enable row level security;
alter table public.user_station_roles force row level security;
create policy user_station_roles_context on public.user_station_roles
  using (org_id::text = current_setting('app.org_id', true)
     and (current_setting('app.station_id', true) = ''
       or station_id::text = current_setting('app.station_id', true)));
create policy user_station_roles_authentication on public.user_station_roles to auth_owner
  using (true);

alter table public.audit_chain_locks enable row level security;
alter table public.audit_chain_locks force row level security;
create policy audit_chain_locks_context on public.audit_chain_locks
  using (org_id::text = current_setting('app.org_id', true));

revoke all on all tables in schema public from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on all sequences in schema public from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on all functions in schema public from public, pomkita_app, report_writer, audit_owner, relay;

grant select on public.users, public.user_station_roles to auth_owner;

alter default privileges revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;

alter default privileges for role org_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role org_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role org_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role station_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role station_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role station_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role user_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role user_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role user_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role auth_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role auth_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role auth_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role registry_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role registry_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role registry_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role audit_lock_owner revoke all on tables from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role audit_lock_owner revoke all on sequences from public, pomkita_app, report_writer, audit_owner, relay;
alter default privileges for role audit_lock_owner revoke all on functions from public, pomkita_app, report_writer, audit_owner, relay;

do $$
declare
  v_owner name;
begin
  for v_owner in
    select rolname from pg_roles
    where rolname in ('pomkita', 'org_owner', 'station_owner', 'user_owner',
                      'auth_owner', 'registry_owner', 'audit_lock_owner')
  loop
    execute format('alter default privileges for role %I revoke all on tables from %I', v_owner, v_owner);
    execute format('alter default privileges for role %I revoke all on sequences from %I', v_owner, v_owner);
    execute format('alter default privileges for role %I revoke all on functions from %I', v_owner, v_owner);
  end loop;
end
$$;

insert into public.procedure_registry (name, allowed_roles, action, lock_rank)
values ('fn_set_request_context', array['pomkita_app'], 'set_request_context', 110);
