-- PLAN.md sections 4.3, 7, 8, and 9; BE-PLAN.md Phase B2.

delete from public.procedure_registry where name in (
  'fn_append_audit_event', 'fn_record_audit_denied', 'read_audit_chain',
  'fn_verify_audit_chain', 'trg_audit_append_only'
  , 'trg_governance_audit'
);
drop trigger if exists amendments_audit on public.amendments;
drop trigger if exists ack_decisions_audit on public.ack_decisions;
drop trigger if exists shift_transitions_audit on public.shift_transitions;
drop trigger if exists audit_denied_guard on public.audit_denied;
drop trigger if exists audit_outbox_guard on public.audit_outbox;
drop function if exists public.fn_record_audit_denied(uuid, uuid, uuid, uuid, uuid, text, text, text, text, text);
drop function if exists public.fn_verify_audit_chain();
drop function if exists public.read_audit_chain();
drop function if exists public.fn_append_audit_event(uuid, text, jsonb, text, text);
drop function if exists public.trg_audit_append_only();
drop function if exists public.trg_governance_audit();
drop table if exists public.audit_denied;
drop table if exists public.audit_outbox;
drop table if exists public.audit_log;
revoke usage on schema public, app from audit_owner;
revoke all on all tables in schema public from audit_owner;
revoke all on all functions in schema public from audit_owner;
