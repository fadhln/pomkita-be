-- PLAN.md section 4.4; BE-PLAN.md Phase B5.

do $$
declare
  v_definition text;
begin
  select pg_get_functiondef(p.oid)
    into v_definition
    from pg_proc p
    join pg_namespace n on n.oid = p.pronamespace
   where n.nspname = 'public'
     and p.proname = 'fn_submit_shift'
     and p.pronargs = 6;
  if v_definition is null then
    raise exception using errcode = '42883', message = 'submit_procedure_missing';
  end if;
  v_definition := replace(v_definition,
    'where t.org_id = o and not t.disabled and t.valid_from <= clock_timestamp() and (t.station_id = st or t.station_id is null)',
    'where t.org_id = o and t.valid_from <= clock_timestamp() and (t.station_id = st or t.station_id is null)');
  v_definition := replace(v_definition,
    'where e.org_id = o and not e.disabled and e.valid_from <= clock_timestamp() and (e.station_id = st or e.station_id is null)',
    'where e.org_id = o and e.valid_from <= clock_timestamp() and (e.station_id = st or e.station_id is null)');
  execute v_definition;
end;
$$;
