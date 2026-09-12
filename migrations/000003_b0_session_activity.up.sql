-- PLAN.md section 7; BE-PLAN.md Phase B0.

alter table public.sessions
  add column last_active_at timestamptz(6) not null default clock_timestamp();

create or replace function public.fn_set_request_context(p_raw_token text)
returns void
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
declare
  v_parts text[];
  v_header jsonb;
  v_claims jsonb;
  v_header_text text;
  v_claims_text text;
  v_signing_input text;
  v_secret text;
  v_signature bytea;
  v_key_secret_ref text;
  v_key_status text;
  v_key_max_token_expiry timestamptz(6);
  v_kid text;
  v_issuer text;
  v_subject uuid;
  v_jti uuid;
  v_issued_at numeric;
  v_expires_at numeric;
  v_now numeric := extract(epoch from clock_timestamp());
  v_now_at timestamptz(6) := clock_timestamp();
  v_session_issued_at timestamptz(6);
  v_session_expires_at timestamptz(6);
  v_session_last_active_at timestamptz(6);
  v_new_session_expires_at timestamptz(6);
  v_revoked_at timestamptz(6);
  v_org_id uuid;
  v_enabled boolean;
  v_role text;
  v_roles text;
begin
  perform set_config('app.context_valid', 'false', true);
  perform set_config('app.org_id', '', true);
  perform set_config('app.station_id', '', true);
  perform set_config('app.user_id', '', true);
  perform set_config('app.jti', '', true);
  perform set_config('app.role', '', true);
  perform set_config('app.roles', '', true);
  perform set_config('app.break_glass_reason', '', true);

  if p_raw_token is null or btrim(p_raw_token) = '' then
    raise exception using errcode = '28000', message = 'jwt_missing_token';
  end if;

  v_parts := string_to_array(p_raw_token, '.');
  if coalesce(array_length(v_parts, 1), 0) <> 3 then
    raise exception using errcode = '28000', message = 'jwt_malformed';
  end if;

  begin
    v_header_text := convert_from(
      decode(
        replace(replace(v_parts[1], '-', '+'), '_', '/')
          || repeat('=', (4 - length(v_parts[1]) % 4) % 4),
        'base64'
      ),
      'UTF8'
    );
    v_claims_text := convert_from(
      decode(
        replace(replace(v_parts[2], '-', '+'), '_', '/')
          || repeat('=', (4 - length(v_parts[2]) % 4) % 4),
        'base64'
      ),
      'UTF8'
    );
    v_header := v_header_text::jsonb;
    v_claims := v_claims_text::jsonb;
    v_signature := decode(
      replace(replace(v_parts[3], '-', '+'), '_', '/')
        || repeat('=', (4 - length(v_parts[3]) % 4) % 4),
      'base64'
    );
  exception when others then
    raise exception using errcode = '28000', message = 'jwt_malformed';
  end;

  if coalesce(v_header->>'alg', '') <> 'HS256' then
    raise exception using errcode = '28000', message = 'jwt_invalid_algorithm';
  end if;
  if coalesce(v_claims->>'iss', '') <> 'pomkita' then
    raise exception using errcode = '28000', message = 'jwt_invalid_issuer';
  end if;
  if not (
    (jsonb_typeof(v_claims->'aud') = 'string' and v_claims->>'aud' = 'spbu-recon')
    or (jsonb_typeof(v_claims->'aud') = 'array' and v_claims->'aud' @> '["spbu-recon"]'::jsonb)
  ) then
    raise exception using errcode = '28000', message = 'jwt_invalid_audience';
  end if;
  if not (v_claims ? 'exp') or coalesce(v_claims->>'exp', '') !~ '^[0-9]+(\.[0-9]+)?$' then
    raise exception using errcode = '28000', message = 'jwt_invalid_exp';
  end if;
  if not (v_claims ? 'iat') or coalesce(v_claims->>'iat', '') !~ '^[0-9]+(\.[0-9]+)?$' then
    raise exception using errcode = '28000', message = 'jwt_invalid_iat';
  end if;
  v_expires_at := (v_claims->>'exp')::numeric;
  v_issued_at := (v_claims->>'iat')::numeric;
  if v_expires_at < v_now - 60 then
    raise exception using errcode = '28000', message = 'jwt_expired';
  end if;
  if v_issued_at > v_now then
    raise exception using errcode = '28000', message = 'jwt_future_issued_at';
  end if;
  if coalesce(v_claims->>'jti', '') !~* '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' then
    raise exception using errcode = '28000', message = 'jwt_invalid_jti';
  end if;
  if coalesce(v_claims->>'sub', '') !~* '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' then
    raise exception using errcode = '28000', message = 'jwt_invalid_subject';
  end if;
  if coalesce(v_header->>'kid', '') = '' then
    raise exception using errcode = '28000', message = 'jwt_unknown_kid';
  end if;

  v_kid := v_header->>'kid';
  v_subject := (v_claims->>'sub')::uuid;
  v_jti := (v_claims->>'jti')::uuid;
  select k.secret_ref, k.status, k.max_token_expiry
    into v_key_secret_ref, v_key_status, v_key_max_token_expiry
    from public.jwt_keys k
   where k.kid = v_kid;
  if not found then
    raise exception using errcode = '28000', message = 'jwt_unknown_kid';
  end if;
  if v_key_status = 'retired'
     or (v_key_status = 'previous' and v_key_max_token_expiry <= v_now_at) then
    raise exception using errcode = '28000', message = 'jwt_retired_key';
  end if;

  v_secret := current_setting(v_key_secret_ref, true);
  if v_secret is null or v_secret = '' then
    raise exception using errcode = '28000', message = 'jwt_invalid_signature';
  end if;
  v_signing_input := v_parts[1] || '.' || v_parts[2];
  if app.hmac(v_signing_input, v_secret, 'sha256') <> v_signature then
    raise exception using errcode = '28000', message = 'jwt_invalid_signature';
  end if;

  select s.issued_at, s.expires_at, s.revoked_at, s.last_active_at
    into v_session_issued_at, v_session_expires_at, v_revoked_at, v_session_last_active_at
    from public.sessions s
   where s.jti = v_jti and s.kid = v_kid
   for update;
  if not found then
    raise exception using errcode = '28000', message = 'jwt_session_not_found';
  end if;
  if v_revoked_at is not null then
    raise exception using errcode = '28000', message = 'jwt_revoked_jti';
  end if;
  if v_session_last_active_at < v_now_at - interval '15 minutes' then
    raise exception using errcode = '28000', message = 'session_idle';
  end if;

  v_new_session_expires_at := least(v_now_at + interval '15 minutes', v_key_max_token_expiry);
  if v_new_session_expires_at <= v_now_at then
    raise exception using errcode = '28000', message = 'jwt_retired_key';
  end if;
  update public.sessions
     set last_active_at = v_now_at,
         issued_at = v_now_at,
         expires_at = v_new_session_expires_at
   where jti = v_jti
     and kid = v_kid
     and revoked_at is null
     and last_active_at >= v_now_at - interval '15 minutes';
  if not found then
    raise exception using errcode = '28000', message = 'session_idle';
  end if;

  select u.org_id, u.enabled
    into v_org_id, v_enabled
    from public.users u
   where u.user_id = v_subject;
  if not found then
    raise exception using errcode = '28000', message = 'jwt_user_not_found';
  end if;
  if not v_enabled then
    raise exception using errcode = '28000', message = 'jwt_user_disabled';
  end if;

  select string_agg(usr.role, ',' order by usr.station_id, usr.role),
         min(usr.role)
    into v_roles, v_role
    from public.user_station_roles usr
   where usr.org_id = v_org_id and usr.user_id = v_subject;

  perform set_config('app.context_valid', 'true', true);
  perform set_config('app.org_id', v_org_id::text, true);
  perform set_config('app.user_id', v_subject::text, true);
  perform set_config('app.jti', v_jti::text, true);
  perform set_config('app.role', coalesce(v_role, ''), true);
  perform set_config('app.roles', coalesce(v_roles, ''), true);
  perform set_config('app.station_id', coalesce(v_claims->>'station_id', ''), true);
  perform set_config('app.break_glass_reason', coalesce(v_claims->>'break_glass_reason', ''), true);
end;
$$;

alter function public.fn_set_request_context(text) owner to auth_owner;
revoke all on function public.fn_set_request_context(text) from public, pomkita_app, report_writer, audit_owner, relay;
grant execute on function public.fn_set_request_context(text) to pomkita_app;
