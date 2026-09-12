-- PLAN.md sections 2 and 7; BE-PLAN.md Phase B0.1.

delete from public.procedure_registry
 where name in (
   'fn_login_user', 'fn_read_jwt_key', 'fn_read_active_jwt_key',
   'fn_create_session', 'fn_read_session_record', 'fn_revoke_session',
   'fn_read_session'
 );

drop function if exists public.fn_login_user(text, text);
drop function if exists public.fn_read_jwt_key(text);
drop function if exists public.fn_read_active_jwt_key();
drop function if exists public.fn_create_session(uuid, text, timestamptz, timestamptz, timestamptz);
drop function if exists public.fn_read_session_record(uuid);
drop function if exists public.fn_revoke_session(uuid, timestamptz);
drop function if exists public.fn_read_session();

alter table public.sessions drop constraint if exists sessions_kid_fkey;
drop index if exists public.users_email_unique_idx;
alter table public.users drop column if exists email, drop column if exists password_hash;
