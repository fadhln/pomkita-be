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
begin
  if exists (select 1 from pg_roles where rolname = 'pomkita_app') then
    revoke usage on schema public from pomkita_app;
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
