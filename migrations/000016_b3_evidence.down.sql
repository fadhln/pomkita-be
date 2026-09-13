-- PLAN.md sections 4.2, 4.4, 6, 7, and 8; BE-PLAN.md Phase B3.

delete from public.procedure_registry
 where name in ('fn_validate_evidence', 'trg_evidence_event_guard',
                'trg_evidence_safety_net', 'trg_loss_exception_guard');
drop trigger if exists loss_exception_evidence_safety on public.loss_exception;
drop trigger if exists shift_report_evidence_safety on public.shift_reports;
drop trigger if exists evidence_event_guard on public.evidence_event;
drop trigger if exists loss_exception_guard on public.loss_exception;
drop function if exists public.trg_evidence_safety_net();
drop function if exists public.trg_evidence_event_guard();
drop function if exists public.trg_loss_exception_guard();
drop function if exists public.fn_validate_evidence(uuid);
drop table if exists public.loss_exception;
drop table if exists public.evidence_event;
