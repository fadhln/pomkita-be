-- PLAN.md sections 3.2, 3.4, 4.2, 4.3, 7, and 8; BE-PLAN.md Phase B2.

delete from public.procedure_registry
 where name in ('fn_ack_shift', 'trg_ack_guard', 'trg_ack_report_head',
                'trg_ack_cardinality', 'trg_ack_supersession_check');
drop trigger if exists ack_supersession_check on public.ack_supersessions;
drop trigger if exists ack_head_cardinality on public.ack_head;
drop trigger if exists shifts_ack_cardinality on public.shifts;
drop trigger if exists shift_reports_ack_head on public.shift_reports;
drop trigger if exists ack_supersessions_guard on public.ack_supersessions;
drop trigger if exists ack_head_guard on public.ack_head;
drop trigger if exists ack_decisions_guard on public.ack_decisions;
drop function if exists public.fn_ack_shift(uuid, uuid, integer, public.ack_decision, text, boolean, text);
drop function if exists public.trg_ack_supersession_check();
drop function if exists public.trg_ack_cardinality();
drop function if exists public.trg_ack_report_head();
drop function if exists public.trg_ack_guard();
drop table if exists public.ack_supersessions;
drop table if exists public.ack_head;
drop table if exists public.ack_decisions;
drop type if exists public.ack_decision;
