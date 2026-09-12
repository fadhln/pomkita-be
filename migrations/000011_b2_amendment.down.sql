-- PLAN.md sections 3.2, 3.3, 4.2, 4.3, 7, and 8; BE-PLAN.md Phase B2.

delete from public.procedure_registry
 where name in ('fn_approve_amendment', 'fn_amendment_base_payload', 'trg_amendment_guard');
drop trigger if exists amendment_items_guard on public.amendment_items;
drop trigger if exists amendments_guard on public.amendments;
drop function if exists public.fn_approve_amendment(uuid, bytea);
drop function if exists public.fn_amendment_base_payload(uuid, uuid, uuid, uuid);
drop function if exists public.trg_amendment_guard();
drop table if exists public.amendment_items;
drop table if exists public.amendments;
