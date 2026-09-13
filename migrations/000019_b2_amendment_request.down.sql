delete from public.procedure_registry
 where name in ('fn_request_amendment', 'read_amendment_queue', 'fn_reject_amendment');
drop function if exists public.fn_request_amendment(uuid, uuid, text, jsonb);
drop function if exists public.fn_reject_amendment(uuid, text);
drop function if exists public.read_amendment_queue();

create or replace function public.trg_amendment_guard()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, public, app
as $$
begin
  if tg_op in ('UPDATE', 'DELETE')
     and current_setting('app.transition', true) <> 'approve_amendment' then
    raise exception using errcode = '23514', message = 'amendment_write_forbidden';
  end if;
  return coalesce(new, old);
end;
$$;
alter function public.trg_amendment_guard() owner to report_writer;
