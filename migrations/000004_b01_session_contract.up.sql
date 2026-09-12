-- PLAN.md sections 2 and 7; BE-PLAN.md Phase B0.1.

alter table public.users
  add column if not exists email text not null,
  add column if not exists password_hash text not null;

create unique index if not exists users_email_unique_idx on public.users (email);

do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'sessions_kid_fkey') then
    alter table public.sessions
      add constraint sessions_kid_fkey
      foreign key (kid) references public.jwt_keys (kid);
  end if;
end $$;

create or replace function public.fn_login_user(p_email text, p_password text)
returns table (user_id uuid)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  select u.user_id
    into user_id
    from public.users u
   where u.email = lower(btrim(p_email))
     and u.enabled
     and u.password_hash = app.crypt(coalesce(p_password, ''), u.password_hash);

  if not found then
    raise exception using errcode = '28000', message = 'invalid_credentials';
  end if;

  return next;
end;
$$;

create or replace function public.fn_read_jwt_key(p_kid text)
returns table (
  kid text,
  secret_ref text,
  status text,
  activated_at timestamptz(6),
  max_token_expiry timestamptz(6)
)
language sql
security definer
set search_path = pg_catalog, public, app
as $$
  select k.kid, k.secret_ref, k.status, k.activated_at, k.max_token_expiry
    from public.jwt_keys k
   where k.kid = p_kid;
$$;

create or replace function public.fn_read_active_jwt_key()
returns table (
  kid text,
  secret_ref text,
  status text,
  activated_at timestamptz(6),
  max_token_expiry timestamptz(6)
)
language sql
security definer
set search_path = pg_catalog, public, app
as $$
  select k.kid, k.secret_ref, k.status, k.activated_at, k.max_token_expiry
    from public.jwt_keys k
   where k.status = 'active'
   order by k.activated_at desc
   limit 1;
$$;

create or replace function public.fn_create_session(
  p_jti uuid,
  p_kid text,
  p_issued_at timestamptz(6),
  p_expires_at timestamptz(6),
  p_last_active_at timestamptz(6)
)
returns void
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  insert into public.sessions (jti, kid, issued_at, expires_at, last_active_at)
  values (p_jti, p_kid, p_issued_at, p_expires_at, p_last_active_at);
end;
$$;

create or replace function public.fn_read_session_record(p_jti uuid)
returns table (
  jti uuid,
  kid text,
  issued_at timestamptz(6),
  expires_at timestamptz(6),
  last_active_at timestamptz(6),
  revoked_at timestamptz(6)
)
language sql
security definer
set search_path = pg_catalog, public, app
as $$
  select s.jti, s.kid, s.issued_at, s.expires_at, s.last_active_at, s.revoked_at
    from public.sessions s
   where s.jti = p_jti;
$$;

create or replace function public.fn_revoke_session(p_jti uuid, p_revoked_at timestamptz(6))
returns void
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  update public.sessions
     set revoked_at = p_revoked_at
   where jti = p_jti
     and revoked_at is null;
  if not found then
    raise exception using errcode = '28000', message = 'invalid_session';
  end if;
end;
$$;

create or replace function public.fn_read_session()
returns table (
  user_id uuid,
  display_name text,
  roles text[],
  org_id uuid,
  station_ids uuid[]
)
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_user_id uuid;
  v_org_id uuid;
  v_display_name text;
begin
  if current_setting('app.context_valid', true) <> 'true'
     or current_setting('app.user_id', true) !~* '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' then
    raise exception using errcode = '28000', message = 'invalid_session';
  end if;

  v_user_id := current_setting('app.user_id', true)::uuid;
  select u.org_id, u.display_name
    into v_org_id, v_display_name
    from public.users u
   where u.user_id = v_user_id
     and u.enabled;
  if not found then
    raise exception using errcode = '28000', message = 'invalid_session';
  end if;

  user_id := v_user_id;
  display_name := v_display_name;
  org_id := v_org_id;
  select coalesce(array_agg(usr.role order by usr.role), '{}'::text[]),
         coalesce(array_agg(distinct usr.station_id order by usr.station_id), '{}'::uuid[])
    into roles, station_ids
    from public.user_station_roles usr
   where usr.org_id = v_org_id
     and usr.user_id = v_user_id;
  return next;
end;
$$;

do $$
declare
  v_name text;
begin
  foreach v_name in array array[
    'fn_login_user', 'fn_read_jwt_key', 'fn_read_active_jwt_key',
    'fn_create_session', 'fn_read_session_record', 'fn_revoke_session',
    'fn_read_session'
  ] loop
    execute format('alter function public.%I(%s) owner to auth_owner', v_name,
      case v_name
        when 'fn_login_user' then 'text, text'
        when 'fn_read_jwt_key' then 'text'
        when 'fn_create_session' then 'uuid, text, timestamptz, timestamptz, timestamptz'
        when 'fn_read_session_record' then 'uuid'
        when 'fn_revoke_session' then 'uuid, timestamptz'
        else ''
      end);
  end loop;
end
$$;

revoke all on function public.fn_login_user(text, text) from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_read_jwt_key(text) from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_read_active_jwt_key() from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_create_session(uuid, text, timestamptz, timestamptz, timestamptz) from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_read_session_record(uuid) from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_revoke_session(uuid, timestamptz) from public, pomkita_app, report_writer, audit_owner, relay;
revoke all on function public.fn_read_session() from public, pomkita_app, report_writer, audit_owner, relay;

grant execute on function public.fn_login_user(text, text) to pomkita_app;
grant execute on function public.fn_read_jwt_key(text) to pomkita_app;
grant execute on function public.fn_read_active_jwt_key() to pomkita_app;
grant execute on function public.fn_create_session(uuid, text, timestamptz, timestamptz, timestamptz) to pomkita_app;
grant execute on function public.fn_read_session_record(uuid) to pomkita_app;
grant execute on function public.fn_revoke_session(uuid, timestamptz) to pomkita_app;
grant execute on function public.fn_read_session() to pomkita_app;

grant execute on function app.crypt(text, text) to auth_owner;

insert into public.procedure_registry (name, allowed_roles, action, lock_rank)
values
  ('fn_login_user', array['pomkita_app'], 'login', 110),
  ('fn_read_jwt_key', array['pomkita_app'], 'read_session', 110),
  ('fn_read_active_jwt_key', array['pomkita_app'], 'read_session', 110),
  ('fn_create_session', array['pomkita_app'], 'create_session', 110),
  ('fn_read_session_record', array['pomkita_app'], 'read_session', 110),
  ('fn_revoke_session', array['pomkita_app'], 'revoke_session', 110),
  ('fn_read_session', array['pomkita_app'], 'read_session', 110)
on conflict (name) do update
  set allowed_roles = excluded.allowed_roles,
      action = excluded.action,
      lock_rank = excluded.lock_rank;
