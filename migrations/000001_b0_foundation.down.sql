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
                      'auth_owner', 'registry_owner', 'audit_lock_owner')
  loop
    execute format('revoke usage on schema public from %I', v_role);
    execute format('revoke usage on schema app from %I', v_role);
  end loop;
  if to_regprocedure('app.hmac(text,text,text)') is not null then
    revoke execute on function app.hmac(text, text, text) from auth_owner;
  end if;
end
$$;

drop role if exists audit_lock_owner;
drop role if exists registry_owner;
drop role if exists auth_owner;
drop role if exists user_owner;
drop role if exists station_owner;
drop role if exists org_owner;
drop role if exists relay;
drop role if exists audit_owner;
drop role if exists report_writer;
drop role if exists pomkita_app;

drop extension if exists pgcrypto;
drop schema if exists app;
