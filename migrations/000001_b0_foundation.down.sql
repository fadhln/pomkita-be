-- PLAN.md sections 4.1 and 7; BE-PLAN.md Phase B0.

drop table if exists public.audit_chain_locks;
drop table if exists public.procedure_registry;
drop table if exists public.jwt_keys;
drop table if exists public.sessions;
drop table if exists public.user_station_roles;
drop table if exists public.users;
drop table if exists public.stations;
drop table if exists public.organizations;

do $$
declare
  v_owner name;
  v_grantee name;
  v_role name;
begin
  if to_regclass('public.users') is not null
     and to_regclass('public.user_station_roles') is not null
     and exists (select 1 from pg_roles where rolname = 'auth_owner') then
    revoke select on public.users, public.user_station_roles from auth_owner;
  end if;
  for v_role in
    select rolname from pg_roles
    where rolname in ('pomkita_app', 'org_owner', 'station_owner', 'user_owner',
                      'auth_owner', 'registry_owner', 'audit_lock_owner', 'report_writer')
  loop
    execute format('revoke usage on schema public from %I', v_role);
    if to_regnamespace('app') is not null then
      execute format('revoke usage on schema app from %I', v_role);
    end if;
  end loop;
  if to_regprocedure('app.hmac(text,text,text)') is not null
     and exists (select 1 from pg_roles where rolname = 'auth_owner') then
    revoke execute on function app.hmac(text, text, text) from auth_owner;
  end if;
  for v_owner in
    select rolname from pg_roles
    where rolname in ('pomkita', 'org_owner', 'station_owner', 'user_owner',
                      'auth_owner', 'registry_owner', 'audit_lock_owner')
  loop
    execute format('alter default privileges for role %I revoke all on tables from public', v_owner);
    execute format('alter default privileges for role %I revoke all on sequences from public', v_owner);
    execute format('alter default privileges for role %I revoke all on functions from public', v_owner);
    for v_grantee in
      select rolname from pg_roles
      where rolname in ('pomkita_app', 'report_writer', 'audit_owner', 'relay')
    loop
      execute format('alter default privileges for role %I revoke all on tables from %I', v_owner, v_grantee);
      execute format('alter default privileges for role %I revoke all on sequences from %I', v_owner, v_grantee);
      execute format('alter default privileges for role %I revoke all on functions from %I', v_owner, v_grantee);
    end loop;
    execute format('alter default privileges for role %I revoke all on tables from %I', v_owner, v_owner);
    execute format('alter default privileges for role %I revoke all on sequences from %I', v_owner, v_owner);
    execute format('alter default privileges for role %I revoke all on functions from %I', v_owner, v_owner);
    if v_owner <> 'pomkita' then
      execute format('drop owned by %I cascade', v_owner);
    end if;
  end loop;
end
$$;

-- Keep the shared extension and schema. Clean-schema tables can depend on them.
