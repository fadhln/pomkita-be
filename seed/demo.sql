-- Development-only demo data. The password is demo-password.
begin;

insert into public.organizations (org_id, name)
values ('11111111-1111-4111-8111-111111111111', 'PomKita Demo')
on conflict (org_id) do nothing;

insert into public.stations (org_id, station_id, timezone)
values ('11111111-1111-4111-8111-111111111111',
        '22222222-2222-4222-8222-222222222222', 'Asia/Jakarta')
on conflict (org_id, station_id) do nothing;

insert into public.users (user_id, org_id, email, username, display_name, password_hash)
values
  ('66666666-6666-4666-8666-666666666666', '11111111-1111-4111-8111-111111111111',
   'supervisor@demo.pomkita.test', 'demo-supervisor', 'Demo Supervisor',
   app.crypt('demo-password', '$2a$04$C6UzMDM.H6dfI/f/IKcEe.')),
  ('77777777-7777-4777-8777-777777777777', '11111111-1111-4111-8111-111111111111',
   'station-admin@demo.pomkita.test', 'demo-station-admin', 'Demo Station Admin',
   app.crypt('demo-password', '$2a$04$C6UzMDM.H6dfI/f/IKcEe.')),
  ('88888888-8888-4888-8888-888888888888', '11111111-1111-4111-8111-111111111111',
   'owner@demo.pomkita.test', 'demo-owner', 'Demo Owner',
   app.crypt('demo-password', '$2a$04$C6UzMDM.H6dfI/f/IKcEe.'))
on conflict (user_id) do nothing;

insert into public.user_station_roles (org_id, station_id, user_id, role)
values
  ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '66666666-6666-4666-8666-666666666666', 'Supervisor'),
  ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '77777777-7777-4777-8777-777777777777', 'Station Admin'),
  ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '88888888-8888-4888-8888-888888888888', 'Owner')
on conflict (org_id, station_id, user_id, role) do nothing;

insert into public.jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry)
values ('demo_key', 'app.jwt_secret.key_1', 'active',
        timestamptz '2026-01-01 00:00:00+00',
        timestamptz '2099-01-01 00:00:00+00')
on conflict (kid) do nothing;

insert into public.audit_chain_locks (org_id)
values ('11111111-1111-4111-8111-111111111111')
on conflict (org_id) do nothing;

insert into public.dispensers (org_id, station_id, dispenser_id)
values ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333')
on conflict (org_id, station_id, dispenser_id) do nothing;

insert into public.tanks (org_id, station_id, tank_id)
values ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '55555555-5555-4555-8555-555555555555')
on conflict (org_id, station_id, tank_id) do nothing;

insert into public.nozzles (org_id, station_id, nozzle_id, dispenser_id, meter_max)
values ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '44444444-4444-4444-8444-444444444444', '33333333-3333-4333-8333-333333333333', 99999.9)
on conflict (org_id, station_id, nozzle_id) do nothing;

insert into public.nozzle_tank_map (map_id, org_id, station_id, nozzle_id, tank_id, valid_period)
values ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', '11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '44444444-4444-4444-8444-444444444444', '55555555-5555-4555-8555-555555555555', tstzrange('2020-01-01', '2035-01-01', '[)'))
on conflict (map_id) do nothing;

insert into public.dispenser_nozzle_map (map_id, org_id, station_id, dispenser_id, nozzle_id, valid_period)
values ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', '11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333', '44444444-4444-4444-8444-444444444444', tstzrange('2020-01-01', '2035-01-01', '[)'))
on conflict (map_id) do nothing;

insert into public.dispenser_prices (price_id, org_id, station_id, nozzle_id, price, valid_period, created_by)
values ('ffffffff-ffff-4fff-8fff-ffffffffffff', '11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '44444444-4444-4444-8444-444444444444', 1000, tstzrange('2020-01-01', '2035-01-01', '[)'), '66666666-6666-4666-8666-666666666666')
on conflict (price_id) do nothing;

insert into public.threshold_policy_revisions (rev_id, policy_id, org_id, valid_from, created_by, variance_rupiah_threshold)
values ('99999999-9999-4999-8999-999999999999', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', '11111111-1111-4111-8111-111111111111', timestamptz '2020-01-01 00:00:00+00', '88888888-8888-4888-8888-888888888888', 0)
on conflict (org_id, rev_id) do nothing;

insert into public.evidence_policy_revisions (rev_id, policy_id, org_id, valid_from, mode, created_by)
values ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', '11111111-1111-4111-8111-111111111111', timestamptz '2020-01-01 00:00:00+00', 'opsional', '88888888-8888-4888-8888-888888888888')
on conflict (org_id, rev_id) do nothing;

commit;
