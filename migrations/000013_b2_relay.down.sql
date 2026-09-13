-- PLAN.md sections 7, 8, and 9; BE-PLAN.md Phase B2.

delete from public.procedure_registry where name in ('fn_relay_claim_event', 'fn_relay_finish_event', 'trg_relay_state_init');
drop trigger if exists audit_outbox_relay_state on public.audit_outbox;
drop function if exists public.fn_relay_finish_event(uuid, uuid, uuid, boolean, text);
drop function if exists public.fn_relay_claim_event(uuid, uuid);
drop function if exists public.trg_relay_state_init();
drop table if exists public.outbox_relay_state;
